package ledger_test

import (
	"context"
	"testing"

	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/ledger"
	"go.uber.org/zap"
)

func TestRegistry_RegisterAndDefault(t *testing.T) {
	log := zap.NewNop()
	reg := ledger.NewRegistry()

	// Empty RPC endpoints → backends operate in their in-memory shadow
	// modes (the documented dev/CI behaviour).
	besu := ledger.NewBesuBackend("", "", nil, log)
	reg.Register(besu)

	immu := ledger.NewImmuDBBackend("", "sentinel", "immudb", log)
	reg.Register(immu)

	if def := reg.Default(); def == nil || def.Kind() != ledger.WriterBesu {
		t.Errorf("expected default to be Besu, got %v", def)
	}
	if err := reg.SetDefault("immudb-default"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if def := reg.Default(); def == nil || def.Kind() != ledger.WriterImmuDB {
		t.Errorf("expected default to be ImmuDB after SetDefault, got %v", def)
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	reg := ledger.NewRegistry()
	if _, err := reg.Get("missing"); err == nil {
		t.Error("expected error for missing writer")
	}
}

func TestBesuBackend_SubmitProducesReceipt(t *testing.T) {
	// Empty endpoint → synthetic submit path.
	b := ledger.NewBesuBackend("", "0xabc", nil, zap.NewNop())
	pkt := &core.Packet{
		PacketID: "pkt_x",
		App:      core.AppContext{AppID: "billing"},
		Action:   core.Action{Risk: core.RiskHigh},
	}
	receipt, err := b.Submit(context.Background(), &ledger.AnchorRequest{
		Packet:     pkt,
		PacketHash: "sha256:aaa",
		BundleHash: "sha256:bbb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ChainTransactionID == "" || receipt.ChainTransactionID[:4] != "evm:" {
		t.Errorf("expected evm:-prefixed tx id, got %q", receipt.ChainTransactionID)
	}
	if receipt.ChainHeight != 1 {
		t.Errorf("expected height 1, got %d", receipt.ChainHeight)
	}
	if receipt.WriterKind != string(ledger.WriterBesu) {
		t.Errorf("expected WriterKind=besu, got %q", receipt.WriterKind)
	}
}

func TestImmuDBBackend_SubmitAndVerify(t *testing.T) {
	// Empty endpoint → in-memory shadow log path.
	b := ledger.NewImmuDBBackend("", "sentinel", "immudb", zap.NewNop())
	pkt := &core.Packet{
		PacketID: "pkt_y",
		App:      core.AppContext{AppID: "ledger-test"},
		Action:   core.Action{Risk: core.RiskMedium},
	}
	receipt, err := b.Submit(context.Background(), &ledger.AnchorRequest{
		Packet:     pkt,
		PacketHash: "sha256:ccc",
		BundleHash: "sha256:ddd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ChainTransactionID[:7] != "immudb:" {
		t.Errorf("expected immudb:-prefixed tx id, got %q", receipt.ChainTransactionID)
	}
	got, err := b.Verify(context.Background(), receipt.ReceiptID)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.PacketID != receipt.PacketID {
		t.Errorf("verify returned wrong packet id: %q", got.PacketID)
	}
}

func TestRegistry_HealthAll(t *testing.T) {
	reg := ledger.NewRegistry()
	reg.Register(ledger.NewBesuBackend("", "", nil, zap.NewNop()))
	reg.Register(ledger.NewImmuDBBackend("", "db", "u", zap.NewNop()))
	statuses := reg.HealthAll(context.Background())
	if len(statuses) != 2 {
		t.Fatalf("expected 2 health entries, got %d", len(statuses))
	}
	for _, s := range statuses {
		if !s.Healthy {
			t.Errorf("writer %q should report healthy: %s", s.Name, s.Reason)
		}
	}
}
