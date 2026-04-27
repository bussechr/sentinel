package ledger_test

import (
	"testing"
	"time"

	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/ledger"
)

func makeTestPacket(risk core.RiskLevel) *core.Packet {
	return &core.Packet{
		SchemaVersion: core.SchemaVersion,
		PacketID:      "pkt_test",
		CorrelationID: "corr_test",
		CapturedAt:    time.Now().UTC(),
		App:           core.AppContext{AppID: "test-app"},
		Action: core.Action{
			Category: core.CategoryHTTP,
			Name:     "test.action",
			Risk:     risk,
			Mutating: false,
		},
		Payload: core.Payload{BodyHash: "sha256:abc", RedactionProfile: "default"},
	}
}

func TestHashPacket_Deterministic(t *testing.T) {
	p := makeTestPacket(core.RiskLow)
	h1, err := ledger.HashPacket(p)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := ledger.HashPacket(p)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("hash not deterministic: %q vs %q", h1, h2)
	}
}

func TestHashPacket_Format(t *testing.T) {
	p := makeTestPacket(core.RiskLow)
	h, err := ledger.HashPacket(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) < 10 || h[:7] != "sha256:" {
		t.Errorf("unexpected hash format: %q", h)
	}
}

func TestHashPacket_ChangesWithPayload(t *testing.T) {
	p1 := makeTestPacket(core.RiskLow)
	p2 := makeTestPacket(core.RiskLow)
	p2.Payload.BodyHash = "sha256:different"

	h1, _ := ledger.HashPacket(p1)
	h2, _ := ledger.HashPacket(p2)
	if h1 == h2 {
		t.Error("hash should differ when payload changes")
	}
}

func TestWitness_IssueAndVerify(t *testing.T) {
	w, err := ledger.NewWitness()
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := w.Issue("pkt_test", "sha256:abc", core.DecisionAllow, core.RiskLow)
	if err != nil {
		t.Fatal(err)
	}

	if receipt.WitnessID == "" {
		t.Error("witness receipt should have an ID")
	}
	if receipt.Signature == "" {
		t.Error("witness receipt should be signed")
	}
	if receipt.Decision != core.DecisionAllow {
		t.Errorf("unexpected decision: %q", receipt.Decision)
	}
}

func TestWitness_DifferentReceiptsPerCall(t *testing.T) {
	w, _ := ledger.NewWitness()
	r1, _ := w.Issue("pkt_1", "sha256:a", core.DecisionAllow, core.RiskLow)
	r2, _ := w.Issue("pkt_2", "sha256:b", core.DecisionAllow, core.RiskLow)

	if r1.WitnessID == r2.WitnessID {
		t.Error("each receipt should have a unique ID")
	}
}
