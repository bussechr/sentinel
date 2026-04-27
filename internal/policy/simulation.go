// Package policy — simulation helpers.
//
// Simulation runs a packet through both the current active bundle and a proposed
// bundle side-by-side and reports whether the decision changes. Used by
// sentinelctl simulate-policy and POST /v1/policy/simulate before bundle promotion.
package policy

// This file is intentionally thin — the core simulation logic lives in Engine.Simulate
// (opa.go). This file documents the simulation workflow and provides the
// SimulationReport type used in API responses.

// SimulationReport is returned to the caller when a bundle promotion is previewed.
// It is identical to SimulationResult but adds bundle metadata for the API layer.
type SimulationReport struct {
	CurrentBundleID  string          `json:"current_bundle_id"`
	ProposedBundleID string          `json:"proposed_bundle_id,omitempty"`
	Current          *EvaluateResult `json:"current"`
	Proposed         *EvaluateResult `json:"proposed,omitempty"`
	Changed          bool            `json:"changed"`
	SafeToPromote    bool            `json:"safe_to_promote"`
}

// IsSafeToPromote returns true when the proposed bundle does not deny
// any packet that was previously allowed (i.e. it is not more restrictive).
func IsSafeToPromote(current, proposed *EvaluateResult) bool {
	if current == nil || proposed == nil {
		return false
	}
	// Promotion is safe if the proposed decision is not more restrictive.
	// allow → warn is considered safe (advisory upgrade).
	// allow → deny is not safe.
	order := map[string]int{"allow": 0, "warn": 1, "deny": 2, "escalate": 3}
	return order[string(proposed.Decision)] <= order[string(current.Decision)]+1
}
