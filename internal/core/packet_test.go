package core_test

import (
	"testing"
	"time"

	"github.com/your-org/sentinel/internal/core"
)

func TestPacketDefaults(t *testing.T) {
	p := core.Packet{
		SchemaVersion: core.SchemaVersion,
		PacketID:      "pkt_test",
		CorrelationID: "corr_test",
		CapturedAt:    time.Now().UTC(),
		App: core.AppContext{
			AppID:       "billing-api",
			Service:     "payments",
			Environment: "test",
			Version:     "1.0.0",
		},
		Actor: core.Actor{
			Type:             core.ActorService,
			IDHash:           "sha256:abc",
			IdentityProvider: core.IDProviderLocal,
		},
		Action: core.Action{
			Category: core.CategoryHTTP,
			Name:     "invoice.refund.create",
			Risk:     core.RiskHigh,
			Mutating: true,
		},
		Resource: core.Resource{
			Type:       "invoice",
			IDHash:     "sha256:def",
			TenantHash: "sha256:ghi",
		},
		Payload: core.Payload{
			BodyHash:         "sha256:jkl",
			RedactionProfile: "default-prod",
		},
	}

	if p.SchemaVersion != "sentinel.packet.v1" {
		t.Errorf("unexpected schema version: %q", p.SchemaVersion)
	}
	if p.Action.Risk != core.RiskHigh {
		t.Errorf("expected risk high, got %q", p.Action.Risk)
	}
	if !p.Action.Mutating {
		t.Error("expected mutating=true")
	}
}

func TestActorTypes(t *testing.T) {
	types := []core.ActorType{
		core.ActorHuman,
		core.ActorService,
		core.ActorAgent,
		core.ActorModel,
		core.ActorSystem,
	}
	for _, at := range types {
		if string(at) == "" {
			t.Errorf("empty actor type: %v", at)
		}
	}
}

func TestRiskLevels(t *testing.T) {
	levels := []core.RiskLevel{
		core.RiskLow,
		core.RiskMedium,
		core.RiskHigh,
		core.RiskCritical,
	}
	for _, r := range levels {
		if string(r) == "" {
			t.Errorf("empty risk level: %v", r)
		}
	}
}

func TestDecisions(t *testing.T) {
	decisions := []core.Decision{
		core.DecisionAllow,
		core.DecisionWarn,
		core.DecisionDeny,
		core.DecisionEscalate,
	}
	for _, d := range decisions {
		if string(d) == "" {
			t.Errorf("empty decision: %v", d)
		}
	}
}
