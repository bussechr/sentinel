package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/ledger"
	"go.uber.org/zap"
)

func TestDurableQueueWritesBeforeLeaderSubmit(t *testing.T) {
	ctx := context.Background()
	store := &fakeDurableStore{}
	backend := &fakeBackend{}
	queue := ledger.NewQueue(backend, zap.NewNop()).
		WithDurableStore(store).
		WithLeaderElector(staticElector{leader: true, id: "api-0"})

	req := testAnchorRequest(core.RiskHigh)
	receipt, err := queue.Enqueue(ctx, req)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if store.enqueued != 1 {
		t.Fatalf("anchor was not durably enqueued before submit: %+v", store)
	}
	if backend.submitted != 1 {
		t.Fatalf("leader should submit high-risk anchor immediately")
	}
	if receipt.Status != core.AnchorAccepted {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	if store.accepted != 1 {
		t.Fatalf("accepted receipt not marked: %+v", store)
	}
}

func TestDurableQueueFollowerDoesNotSubmit(t *testing.T) {
	ctx := context.Background()
	store := &fakeDurableStore{}
	backend := &fakeBackend{}
	queue := ledger.NewQueue(backend, zap.NewNop()).
		WithDurableStore(store).
		WithLeaderElector(staticElector{leader: false, id: "api-1"})

	receipt, err := queue.Enqueue(ctx, testAnchorRequest(core.RiskHigh))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if backend.submitted != 0 {
		t.Fatalf("follower must not submit anchors")
	}
	if receipt.Status != core.AnchorQueued {
		t.Fatalf("follower should return provisional queued receipt: %+v", receipt)
	}
}

func TestDurableQueueDrainRetriesOnWriterFailure(t *testing.T) {
	ctx := context.Background()
	store := &fakeDurableStore{claimed: []*ledger.StoredAnchorRequest{
		{ID: 1, AnchorRequest: testAnchorRequest(core.RiskLow)},
	}}
	backend := &fakeBackend{err: errors.New("rpc unavailable")}
	queue := ledger.NewQueue(backend, zap.NewNop()).
		WithDurableStore(store).
		WithLeaderElector(staticElector{leader: true, id: "api-0"})

	queue.DrainBatch(ctx)
	if backend.submitted != 1 {
		t.Fatalf("leader did not drain claimed row")
	}
	if store.failed != 1 || store.accepted != 0 {
		t.Fatalf("failed row should be released for retry: %+v", store)
	}
}

func testAnchorRequest(risk core.RiskLevel) *ledger.AnchorRequest {
	p := makeTestPacket(risk)
	p.Ledger.AnchorMode = core.AnchorBatch
	if risk == core.RiskHigh || risk == core.RiskCritical {
		p.Ledger.AnchorMode = core.AnchorImmediate
	}
	return &ledger.AnchorRequest{
		Packet:       p,
		PacketHash:   "sha256:packet",
		DecisionHash: "sha256:decision",
		BundleHash:   "sha256:bundle",
	}
}

type staticElector struct {
	leader bool
	id     string
}

func (e staticElector) IsLeader(context.Context) bool { return e.leader }
func (e staticElector) Identity() string              { return e.id }

type fakeBackend struct {
	submitted int
	err       error
}

func (b *fakeBackend) Submit(_ context.Context, req *ledger.AnchorRequest) (*core.Receipt, error) {
	b.submitted++
	if b.err != nil {
		return nil, b.err
	}
	return &core.Receipt{
		ReceiptID:          "rcpt_test",
		PacketID:           req.Packet.PacketID,
		CorrelationID:      req.Packet.CorrelationID,
		PacketHash:         req.PacketHash,
		DecisionHash:       req.DecisionHash,
		PolicyBundleHash:   req.BundleHash,
		AppID:              req.Packet.App.AppID,
		Risk:               req.Packet.Action.Risk,
		AnchorMode:         req.Packet.Ledger.AnchorMode,
		Status:             core.AnchorAccepted,
		ChainTransactionID: "tx_test",
		IssuedAt:           time.Now().UTC(),
	}, nil
}

func (b *fakeBackend) Verify(context.Context, string) (*core.Receipt, error) { return nil, nil }
func (b *fakeBackend) LatestHeight(context.Context) (int64, error)           { return 1, nil }

type fakeDurableStore struct {
	enqueued int
	accepted int
	failed   int
	claimed  []*ledger.StoredAnchorRequest
}

func (s *fakeDurableStore) EnqueueAnchor(context.Context, *ledger.AnchorRequest) (int64, error) {
	s.enqueued++
	return int64(s.enqueued), nil
}

func (s *fakeDurableStore) ClaimAnchorBatch(context.Context, int) ([]*ledger.StoredAnchorRequest, error) {
	return s.claimed, nil
}

func (s *fakeDurableStore) MarkAnchorAccepted(context.Context, int64, *core.Receipt) error {
	s.accepted++
	return nil
}

func (s *fakeDurableStore) MarkAnchorFailed(context.Context, int64, error) error {
	s.failed++
	return nil
}

func (s *fakeDurableStore) PendingAnchorDepth(context.Context) (int64, error) {
	return int64(s.enqueued - s.accepted), nil
}
