package policy_test

import (
	"testing"
	"time"

	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/policy"
)

func makePacket(category core.ActionCategory, risk core.RiskLevel, mutating bool) *core.Packet {
	return &core.Packet{
		SchemaVersion: core.SchemaVersion,
		PacketID:      "pkt_test",
		CapturedAt:    time.Now().UTC(),
		App:           core.AppContext{AppID: "test-app"},
		Action: core.Action{
			Category: category,
			Name:     "test.action",
			Risk:     risk,
			Mutating: mutating,
		},
		Resource: core.Resource{Type: "test_resource"},
	}
}

func TestClassify_SecretAlwaysHigh(t *testing.T) {
	p := makePacket(core.CategorySecret, core.RiskLow, false)
	got := policy.Classify(p)
	if got != core.RiskHigh {
		t.Errorf("secret access: expected high, got %q", got)
	}
}

func TestClassify_AIToolMinimumMedium(t *testing.T) {
	p := makePacket(core.CategoryAI, core.RiskLow, false)
	p.AI = core.AIRecord{IsAIRelated: true, ToolCallCount: 1}
	got := policy.Classify(p)
	if got != core.RiskMedium {
		t.Errorf("ai tool call: expected medium, got %q", got)
	}
}

func TestClassify_K8sAlwaysHigh(t *testing.T) {
	p := makePacket(core.CategoryK8s, core.RiskLow, true)
	got := policy.Classify(p)
	if got != core.RiskHigh {
		t.Errorf("k8s action: expected high, got %q", got)
	}
}

func TestClassify_NeverLowers(t *testing.T) {
	// Declared critical stays critical even if no rules match.
	p := makePacket(core.CategoryHTTP, core.RiskCritical, false)
	got := policy.Classify(p)
	if got != core.RiskCritical {
		t.Errorf("critical should not be lowered, got %q", got)
	}
}

func TestClassify_MutatingCustomerRecord(t *testing.T) {
	p := makePacket(core.CategoryDB, core.RiskLow, true)
	p.Resource.Type = "customer_record"
	got := policy.Classify(p)
	if got != core.RiskHigh {
		t.Errorf("mutating customer record: expected high, got %q", got)
	}
}

func TestMasking_DefaultProd(t *testing.T) {
	p := makePacket(core.CategoryHTTP, core.RiskLow, false)
	p.Actor.IDHash = "sha256:sensitive"
	p.Resource.TenantHash = "sha256:tenant"
	p.Payload.BodyHash = "sha256:body"
	p.CorrelationID = "corr_visible"

	masked := policy.Apply(p, "default-prod")
	if masked.Actor.IDHash != "redacted" {
		t.Errorf("actor id_hash should be redacted, got %q", masked.Actor.IDHash)
	}
	if masked.Resource.TenantHash != "redacted" {
		t.Errorf("tenant hash should be redacted")
	}
	// CorrelationID should NOT be masked by default-prod.
	if masked.CorrelationID != "corr_visible" {
		t.Errorf("correlation ID should not be redacted by default-prod")
	}
	// Original should be unchanged.
	if p.Actor.IDHash != "sha256:sensitive" {
		t.Error("original packet should not be mutated by masking")
	}
}

func TestMasking_PIIStrict(t *testing.T) {
	p := makePacket(core.CategoryHTTP, core.RiskLow, false)
	p.CorrelationID = "corr_secret"

	masked := policy.Apply(p, "pii-strict")
	if masked.CorrelationID != "redacted" {
		t.Errorf("correlation ID should be redacted in pii-strict, got %q", masked.CorrelationID)
	}
}

func TestMasking_UnknownProfileDefaultsProd(t *testing.T) {
	p := makePacket(core.CategoryHTTP, core.RiskLow, false)
	p.Actor.IDHash = "sha256:sensitive"

	masked := policy.Apply(p, "nonexistent-profile")
	if masked.Actor.IDHash != "redacted" {
		t.Error("unknown profile should default to default-prod masking")
	}
}
