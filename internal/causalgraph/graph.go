// Package causalgraph turns rewind output into a queryable DAG keyed by
// correlation_id.
//
// The rewind engine fetches packets, decisions, receipts, AI traces, and
// runtime segments. Causalgraph normalises those items into typed nodes
// and edges so the incident response UI and sentinelctl can render a
// timeline, find the chain root cause for a deny, and diff against
// shadow policy results.
//
// The graph is intentionally in-memory and stateless: build it from the
// rewind result, query it, drop it. Persistence happens in Postgres via
// the existing rewind store.
package causalgraph

import (
	"sort"
	"time"

	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/evidence"
)

// NodeKind classifies a node in the causal graph.
type NodeKind string

const (
	NodePacket   NodeKind = "packet"
	NodeDecision NodeKind = "decision"
	NodeReceipt  NodeKind = "receipt"
	NodeSegment  NodeKind = "segment"
	NodeAI       NodeKind = "ai"
)

// EdgeKind classifies the relationship between two nodes.
type EdgeKind string

const (
	EdgeEvaluatedAs EdgeKind = "evaluated_as" // packet → decision
	EdgeAnchoredBy  EdgeKind = "anchored_by"  // packet → receipt
	EdgeObservedBy  EdgeKind = "observed_by"  // packet → segment
	EdgeFollowedBy  EdgeKind = "followed_by"  // packet[t] → packet[t+1] within correlation_id
)

// Node is a vertex in the causal graph.
type Node struct {
	ID    string      `json:"id"`
	Kind  NodeKind    `json:"kind"`
	At    time.Time   `json:"at"`
	Label string      `json:"label"`
	Data  interface{} `json:"data,omitempty"`
}

// Edge connects two nodes.
type Edge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Kind EdgeKind `json:"kind"`
}

// Graph is the compiled DAG for one correlation_id.
type Graph struct {
	CorrelationID string  `json:"correlation_id"`
	Nodes         []*Node `json:"nodes"`
	Edges         []*Edge `json:"edges"`
}

// Result is the input bundle the compiler accepts. It is the same shape
// as evidence.RewindResult; we re-declare it locally so the compiler is
// not forced to import every evidence consumer.
type Result struct {
	CorrelationID string
	Packets       []*core.Packet
	Decisions     []*core.DecisionRecord
	Receipts      []*core.Receipt
	Segments      []*evidence.Segment
}

// Compile builds a Graph from a rewind result.
//
// Edges are created only when the join keys actually match — a missing
// receipt or decision does not produce a dangling edge, so the resulting
// graph is always well-formed.
func Compile(r Result) *Graph {
	g := &Graph{
		CorrelationID: r.CorrelationID,
	}

	// Index packets by ID so decisions/receipts can attach.
	pktByID := make(map[string]*core.Packet, len(r.Packets))
	for _, p := range r.Packets {
		pktByID[p.PacketID] = p
		g.Nodes = append(g.Nodes, &Node{
			ID:    "pkt:" + p.PacketID,
			Kind:  NodePacket,
			At:    p.CapturedAt,
			Label: p.Action.Name,
			Data:  p,
		})
		if p.AI.IsAIRelated {
			aiID := "ai:" + p.PacketID
			g.Nodes = append(g.Nodes, &Node{
				ID:    aiID,
				Kind:  NodeAI,
				At:    p.CapturedAt,
				Label: "ai:" + p.Action.Name,
				Data:  p.AI,
			})
		}
	}

	// Decisions → packet edge.
	for _, d := range r.Decisions {
		decNode := &Node{
			ID:    "dec:" + d.DecisionID,
			Kind:  NodeDecision,
			At:    d.EvaluatedAt,
			Label: string(d.Decision),
			Data:  d,
		}
		g.Nodes = append(g.Nodes, decNode)
		if _, ok := pktByID[d.PacketID]; ok {
			g.Edges = append(g.Edges, &Edge{
				From: "pkt:" + d.PacketID,
				To:   decNode.ID,
				Kind: EdgeEvaluatedAs,
			})
		}
	}

	// Receipts → packet edge.
	for _, rc := range r.Receipts {
		rcNode := &Node{
			ID:    "rcpt:" + rc.ReceiptID,
			Kind:  NodeReceipt,
			At:    rc.IssuedAt,
			Label: string(rc.Status),
			Data:  rc,
		}
		g.Nodes = append(g.Nodes, rcNode)
		if _, ok := pktByID[rc.PacketID]; ok {
			g.Edges = append(g.Edges, &Edge{
				From: "pkt:" + rc.PacketID,
				To:   rcNode.ID,
				Kind: EdgeAnchoredBy,
			})
		}
	}

	// Segments — link to all packets in the segment's app within the
	// time window. We do not store per-packet segment refs in the hot
	// index, so this is a coarse "observed by" relationship.
	for _, seg := range r.Segments {
		segNode := &Node{
			ID:    "seg:" + seg.SegmentID,
			Kind:  NodeSegment,
			At:    seg.FromTS,
			Label: seg.AppID,
			Data:  seg,
		}
		g.Nodes = append(g.Nodes, segNode)
		for _, p := range r.Packets {
			if p.App.AppID != seg.AppID {
				continue
			}
			if p.CapturedAt.Before(seg.FromTS) || p.CapturedAt.After(seg.ToTS) {
				continue
			}
			g.Edges = append(g.Edges, &Edge{
				From: "pkt:" + p.PacketID,
				To:   segNode.ID,
				Kind: EdgeObservedBy,
			})
		}
	}

	// Causal "followed by" chain across packets ordered by time.
	ordered := make([]*core.Packet, len(r.Packets))
	copy(ordered, r.Packets)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].CapturedAt.Before(ordered[j].CapturedAt)
	})
	for i := 1; i < len(ordered); i++ {
		g.Edges = append(g.Edges, &Edge{
			From: "pkt:" + ordered[i-1].PacketID,
			To:   "pkt:" + ordered[i].PacketID,
			Kind: EdgeFollowedBy,
		})
	}

	return g
}

// FirstDeny returns the earliest decision node whose decision is "deny",
// or nil if none. Useful for the runbook's "what blocked this?" query.
func (g *Graph) FirstDeny() *Node {
	var earliest *Node
	for _, n := range g.Nodes {
		if n.Kind != NodeDecision {
			continue
		}
		d, ok := n.Data.(*core.DecisionRecord)
		if !ok || d.Decision != core.DecisionDeny {
			continue
		}
		if earliest == nil || n.At.Before(earliest.At) {
			earliest = n
		}
	}
	return earliest
}

// Anchored returns true if at least one receipt node has Status anchored
// or accepted. Used by the rewind report to flag unanchored evidence.
func (g *Graph) Anchored() bool {
	for _, n := range g.Nodes {
		if n.Kind != NodeReceipt {
			continue
		}
		rc, ok := n.Data.(*core.Receipt)
		if !ok {
			continue
		}
		if rc.Status == core.AnchorAnchored || rc.Status == core.AnchorAccepted {
			return true
		}
	}
	return false
}

// NodesByKind returns nodes of one kind in chronological order.
func (g *Graph) NodesByKind(kind NodeKind) []*Node {
	var out []*Node
	for _, n := range g.Nodes {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].At.Before(out[j].At)
	})
	return out
}
