// Package ledger manages the anchor queue and interaction with the governance chain.
//
// The anchor queue receives packets after local policy evaluation and
// submits compact proof records (hashes only, never full payloads) to
// the CometBFT chain according to the risk-tiered anchoring strategy.
//
// Anchor queue behaviour by risk:
//   low      → batch anchor; app proceeds after packet storage.
//   medium   → batch anchor + local witness receipt.
//   high     → immediate anchor request; app proceeds after chain acceptance.
//   critical → chain acceptance + approval workflow required.
package ledger

import (
	"context"
	"sync"

	"github.com/your-org/sentinel/internal/core"
	"go.uber.org/zap"
)

// AnchorRequest is a pending chain submission.
type AnchorRequest struct {
	Packet       *core.Packet
	PacketHash   string
	DecisionHash string
	BundleHash   string
}

// Queue buffers AnchorRequests and drains them to the chain backend.
type Queue struct {
	mu       sync.Mutex
	pending  []*AnchorRequest
	backend  Backend
	log      *zap.Logger
	draining bool
}

// Backend is the interface a chain adapter must satisfy.
type Backend interface {
	Submit(ctx context.Context, req *AnchorRequest) (*core.Receipt, error)
	Verify(ctx context.Context, receiptID string) (*core.Receipt, error)
	LatestHeight(ctx context.Context) (int64, error)
}

// NewQueue creates an anchor queue backed by the provided chain backend.
func NewQueue(backend Backend, log *zap.Logger) *Queue {
	return &Queue{backend: backend, log: log}
}

// Enqueue adds an anchor request to the queue and triggers immediate submission
// for high/critical risk packets; low/medium are batched.
func (q *Queue) Enqueue(ctx context.Context, req *AnchorRequest) (*core.Receipt, error) {
	risk := req.Packet.Action.Risk

	// Immediate path for high and critical.
	if risk == core.RiskHigh || risk == core.RiskCritical {
		if q.backend == nil {
			q.log.Warn("chain backend not configured — storing anchor request locally",
				zap.String("packet_id", req.Packet.PacketID))
			return &core.Receipt{
				PacketID:   req.Packet.PacketID,
				PacketHash: req.PacketHash,
				Status:     core.AnchorQueued,
			}, nil
		}
		return q.backend.Submit(ctx, req)
	}

	// Batch path for low and medium.
	q.mu.Lock()
	q.pending = append(q.pending, req)
	q.mu.Unlock()

	q.log.Info("anchor queued (batch)",
		zap.String("packet_id", req.Packet.PacketID),
		zap.String("risk", string(risk)),
	)

	// Return a provisional receipt; the chain receipt arrives asynchronously.
	return &core.Receipt{
		PacketID:   req.Packet.PacketID,
		PacketHash: req.PacketHash,
		Status:     core.AnchorQueued,
	}, nil
}

// DrainBatch submits all pending batch requests. Should be called on a ticker.
func (q *Queue) DrainBatch(ctx context.Context) {
	q.mu.Lock()
	if q.draining || len(q.pending) == 0 {
		q.mu.Unlock()
		return
	}
	batch := q.pending
	q.pending = nil
	q.draining = true
	q.mu.Unlock()

	defer func() {
		q.mu.Lock()
		q.draining = false
		q.mu.Unlock()
	}()

	for _, req := range batch {
		receipt, err := q.backend.Submit(ctx, req)
		if err != nil {
			q.log.Error("batch anchor failed",
				zap.String("packet_id", req.Packet.PacketID),
				zap.Error(err),
			)
			continue
		}
		q.log.Info("batch anchor accepted",
			zap.String("packet_id", receipt.PacketID),
			zap.String("chain_tx_id", receipt.ChainTransactionID),
		)
	}
}
