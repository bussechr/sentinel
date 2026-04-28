package policy_test

import (
	"testing"

	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/policy"
)

func TestShadowResult_ToRecord(t *testing.T) {
	res := &policy.ShadowResult{
		ShadowID:          "shd_x",
		ActiveBundleID:    "active",
		CandidateBundleID: "candidate",
		Active: &policy.EvaluateResult{
			Decision: core.DecisionAllow,
			Reason:   "ok",
		},
		Candidate: &policy.EvaluateResult{
			Decision: core.DecisionDeny,
			Reason:   "policy.bad",
		},
		Diverged: true,
	}
	rec := res.ToRecord("pkt_x", "corr_x")
	if rec.PacketID != "pkt_x" || rec.CorrelationID != "corr_x" {
		t.Errorf("packet/correlation not propagated: %+v", rec)
	}
	if rec.ActiveDecision != core.DecisionAllow {
		t.Errorf("active decision wrong: %v", rec.ActiveDecision)
	}
	if rec.CandidateDecision != core.DecisionDeny {
		t.Errorf("candidate decision wrong: %v", rec.CandidateDecision)
	}
	if !rec.Diverged {
		t.Error("expected diverged=true")
	}
}

func TestShadow_NilSafe(t *testing.T) {
	var s *policy.Shadow
	if s.CandidateConfigured() {
		t.Error("nil shadow should not be configured")
	}
}
