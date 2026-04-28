package causalgraph_test

import (
	"testing"
	"time"

	"github.com/your-org/sentinel/internal/causalgraph"
	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/evidence"
)

func TestCompile_BasicShape(t *testing.T) {
	now := time.Now().UTC()
	pkt := &core.Packet{
		PacketID:      "pkt_1",
		CorrelationID: "corr_1",
		CapturedAt:    now,
		App:           core.AppContext{AppID: "billing"},
		Action:        core.Action{Name: "refund.create", Risk: core.RiskHigh},
	}
	dec := &core.DecisionRecord{
		DecisionID:  "dec_1",
		PacketID:    "pkt_1",
		Decision:    core.DecisionAllow,
		EvaluatedAt: now.Add(time.Millisecond),
	}
	rcp := &core.Receipt{
		ReceiptID: "rcpt_1",
		PacketID:  "pkt_1",
		Status:    core.AnchorAccepted,
		IssuedAt:  now.Add(2 * time.Millisecond),
	}

	g := causalgraph.Compile(causalgraph.Result{
		CorrelationID: "corr_1",
		Packets:       []*core.Packet{pkt},
		Decisions:     []*core.DecisionRecord{dec},
		Receipts:      []*core.Receipt{rcp},
	})

	if g.CorrelationID != "corr_1" {
		t.Fatalf("correlation id mismatch: %q", g.CorrelationID)
	}
	if len(g.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 2 {
		t.Fatalf("expected 2 edges (evaluated_as + anchored_by), got %d", len(g.Edges))
	}
	if !g.Anchored() {
		t.Error("graph should be flagged anchored")
	}
}

func TestCompile_FirstDeny(t *testing.T) {
	now := time.Now().UTC()
	pkts := []*core.Packet{
		{PacketID: "p1", CapturedAt: now, App: core.AppContext{AppID: "a"}},
		{PacketID: "p2", CapturedAt: now.Add(time.Second), App: core.AppContext{AppID: "a"}},
	}
	decs := []*core.DecisionRecord{
		{DecisionID: "d1", PacketID: "p1", Decision: core.DecisionAllow, EvaluatedAt: now},
		{DecisionID: "d2", PacketID: "p2", Decision: core.DecisionDeny, EvaluatedAt: now.Add(time.Second)},
	}
	g := causalgraph.Compile(causalgraph.Result{Packets: pkts, Decisions: decs})

	deny := g.FirstDeny()
	if deny == nil {
		t.Fatal("expected a deny node")
	}
	if deny.ID != "dec:d2" {
		t.Errorf("expected dec:d2, got %s", deny.ID)
	}
}

func TestCompile_FollowedByChain(t *testing.T) {
	now := time.Now().UTC()
	pkts := []*core.Packet{
		{PacketID: "p3", CapturedAt: now.Add(2 * time.Second), App: core.AppContext{AppID: "a"}},
		{PacketID: "p1", CapturedAt: now, App: core.AppContext{AppID: "a"}},
		{PacketID: "p2", CapturedAt: now.Add(time.Second), App: core.AppContext{AppID: "a"}},
	}
	g := causalgraph.Compile(causalgraph.Result{Packets: pkts})

	var followed int
	for _, e := range g.Edges {
		if e.Kind == causalgraph.EdgeFollowedBy {
			followed++
		}
	}
	if followed != 2 {
		t.Errorf("expected 2 followed_by edges, got %d", followed)
	}
}

func TestCompile_SegmentObservation(t *testing.T) {
	now := time.Now().UTC()
	pkt := &core.Packet{
		PacketID:   "p1",
		CapturedAt: now,
		App:        core.AppContext{AppID: "billing"},
	}
	seg := &evidence.Segment{
		SegmentID: "seg_1",
		AppID:     "billing",
		FromTS:    now.Add(-time.Minute),
		ToTS:      now.Add(time.Minute),
	}
	g := causalgraph.Compile(causalgraph.Result{
		Packets:  []*core.Packet{pkt},
		Segments: []*evidence.Segment{seg},
	})

	var observed int
	for _, e := range g.Edges {
		if e.Kind == causalgraph.EdgeObservedBy {
			observed++
		}
	}
	if observed != 1 {
		t.Errorf("expected 1 observed_by edge, got %d", observed)
	}
}
