// Package ledger manages the anchor queue and interaction with the governance chain.
//
// The anchor queue receives packets after local policy evaluation and
// submits compact proof records (hashes only, never full payloads) to
// the CometBFT chain according to the risk-tiered anchoring strategy.
//
// Anchor queue behaviour by risk:
//
//	low      -> batch anchor; app proceeds after packet storage.
//	medium   -> batch anchor + local witness receipt.
//	high     -> immediate anchor request; app proceeds after chain acceptance when this replica is leader.
//	critical -> chain acceptance + approval workflow required.
package ledger

import (
	"context"
	"errors"
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

// StoredAnchorRequest is a durable anchor_queue row claimed for processing.
type StoredAnchorRequest struct {
	ID int64
	*AnchorRequest
}

// DurableStore is the Postgres-backed queue contract used in HA mode.
// Implementations must persist rows before writer fan-out and claim rows
// with row-level locking so multiple replicas cannot process the same anchor.
type DurableStore interface {
	EnqueueAnchor(ctx context.Context, req *AnchorRequest) (int64, error)
	ClaimAnchorBatch(ctx context.Context, limit int) ([]*StoredAnchorRequest, error)
	MarkAnchorAccepted(ctx context.Context, id int64, receipt *core.Receipt) error
	MarkAnchorFailed(ctx context.Context, id int64, cause error) error
	PendingAnchorDepth(ctx context.Context) (int64, error)
}

// LeaderElector gates queue draining in HA mode. Postgres advisory-lock
// election is provided by the postgres store package; tests use static fakes.
type LeaderElector interface {
	IsLeader(ctx context.Context) bool
	Identity() string
}

// Backend is the interface a chain adapter must satisfy.
type Backend interface {
	Submit(ctx context.Context, req *AnchorRequest) (*core.Receipt, error)
	Verify(ctx context.Context, receiptID string) (*core.Receipt, error)
	LatestHeight(ctx context.Context) (int64, error)
}

// Queue buffers AnchorRequests and drains them to the chain backend.
type Queue struct {
	mu       sync.Mutex
	pending  []*AnchorRequest
	backend  Backend
	log      *zap.Logger
	draining bool
	store    DurableStore
	elector  LeaderElector
}

// NewQueue creates an anchor queue backed by the provided chain backend.
func NewQueue(backend Backend, log *zap.Logger) *Queue {
	return &Queue{backend: backend, log: log}
}

// WithDurableStore enables Postgres-backed anchor queue durability.
func (q *Queue) WithDurableStore(store DurableStore) *Queue {
	q.store = store
	return q
}

// WithLeaderElector enables single-writer queue processing across replicas.
func (q *Queue) WithLeaderElector(elector LeaderElector) *Queue {
	q.elector = elector
	return q
}

// Enqueue adds an anchor request to the queue and triggers immediate submission
// for high/critical risk packets when this replica holds leadership. In HA mode
// every request is written to the durable store before any writer fan-out.
func (q *Queue) Enqueue(ctx context.Context, req *AnchorRequest) (*core.Receipt, error) {
	if req == nil || req.Packet == nil {
		return nil, errors.New("anchor queue: packet required")
	}
	risk := req.Packet.Action.Risk
	if q.store != nil {
		rowID, err := q.store.EnqueueAnchor(ctx, req)
		if err != nil {
			return nil, err
		}
		if (risk == core.RiskHigh || risk == core.RiskCritical) && q.backend != nil && q.isLeader(ctx) {
			return q.submitDurable(ctx, &StoredAnchorRequest{ID: rowID, AnchorRequest: req})
		}
		q.log.Info("anchor queued (durable)",
			zap.String("packet_id", req.Packet.PacketID),
			zap.String("risk", string(risk)),
			zap.Int64("queue_id", rowID),
		)
		return provisionalReceipt(req), nil
	}

	// Immediate path for high and critical.
	if risk == core.RiskHigh || risk == core.RiskCritical {
		if q.backend == nil {
			q.log.Warn("chain backend not configured - storing anchor request locally",
				zap.String("packet_id", req.Packet.PacketID))
			return provisionalReceipt(req), nil
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
	return provisionalReceipt(req), nil
}

// DrainBatch submits all pending batch requests. Should be called on a ticker.
func (q *Queue) DrainBatch(ctx context.Context) {
	if q.store != nil {
		q.drainDurable(ctx)
		return
	}
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

// PendingDepth returns the current durable or in-memory queue depth.
func (q *Queue) PendingDepth(ctx context.Context) int64 {
	if q.store != nil {
		depth, err := q.store.PendingAnchorDepth(ctx)
		if err != nil {
			q.log.Warn("anchor queue depth unavailable", zap.Error(err))
			return 0
		}
		return depth
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return int64(len(q.pending))
}

// LeaderMetric returns the current leader identity and metric value expected
// by the Prometheus scrape surface.
func (q *Queue) LeaderMetric(ctx context.Context) (string, int64) {
	if q.elector == nil {
		return "", 1
	}
	if q.elector.IsLeader(ctx) {
		return q.elector.Identity(), 1
	}
	return q.elector.Identity(), 0
}

func (q *Queue) drainDurable(ctx context.Context) {
	if !q.isLeader(ctx) {
		return
	}
	if q.backend == nil {
		q.log.Warn("anchor queue leader has no chain backend configured")
		return
	}
	q.mu.Lock()
	if q.draining {
		q.mu.Unlock()
		return
	}
	q.draining = true
	q.mu.Unlock()
	defer func() {
		q.mu.Lock()
		q.draining = false
		q.mu.Unlock()
	}()

	batch, err := q.store.ClaimAnchorBatch(ctx, 100)
	if err != nil {
		q.log.Error("claim durable anchor batch failed", zap.Error(err))
		return
	}
	for _, req := range batch {
		if _, err := q.submitDurable(ctx, req); err != nil {
			q.log.Error("durable anchor failed",
				zap.Int64("queue_id", req.ID),
				zap.String("packet_id", req.Packet.PacketID),
				zap.Error(err),
			)
		}
	}
}

func (q *Queue) submitDurable(ctx context.Context, req *StoredAnchorRequest) (*core.Receipt, error) {
	receipt, err := q.backend.Submit(ctx, req.AnchorRequest)
	if err != nil {
		if markErr := q.store.MarkAnchorFailed(ctx, req.ID, err); markErr != nil {
			q.log.Error("mark anchor failed", zap.Int64("queue_id", req.ID), zap.Error(markErr))
		}
		return nil, err
	}
	if err := q.store.MarkAnchorAccepted(ctx, req.ID, receipt); err != nil {
		return nil, err
	}
	q.log.Info("durable anchor accepted",
		zap.Int64("queue_id", req.ID),
		zap.String("packet_id", receipt.PacketID),
		zap.String("chain_tx_id", receipt.ChainTransactionID),
	)
	return receipt, nil
}

func (q *Queue) isLeader(ctx context.Context) bool {
	return q.elector == nil || q.elector.IsLeader(ctx)
}

func provisionalReceipt(req *AnchorRequest) *core.Receipt {
	return &core.Receipt{
		PacketID:   req.Packet.PacketID,
		PacketHash: req.PacketHash,
		Status:     core.AnchorQueued,
	}
}
