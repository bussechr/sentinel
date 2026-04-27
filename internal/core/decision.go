// Package core — decision types.
//
// A Decision record is the persisted outcome of a single policy evaluation.
// It links the packet to the OPA bundle revision, the decision log entry
// produced by OPA, and the masking profile applied to the response.
package core

import "time"

// DecisionRecord is the stored outcome of evaluating a Packet against policy.
type DecisionRecord struct {
	DecisionID         string    `json:"decision_id" db:"decision_id"`
	PacketID           string    `json:"packet_id" db:"packet_id"`
	AppID              string    `json:"app_id" db:"app_id"`
	Decision           Decision  `json:"decision" db:"decision"`
	Reason             string    `json:"reason" db:"reason"`
	BundleID           string    `json:"bundle_id" db:"bundle_id"`
	BundleHash         string    `json:"bundle_hash" db:"bundle_hash"`
	DecisionHash       string    `json:"decision_hash" db:"decision_hash"`
	MaskingProfile     string    `json:"masking_profile,omitempty" db:"masking_profile"`
	EvaluatedAt        time.Time `json:"evaluated_at" db:"evaluated_at"`
	OPADecisionLogPath string    `json:"opa_decision_log_path,omitempty" db:"opa_decision_log_path"`
}
