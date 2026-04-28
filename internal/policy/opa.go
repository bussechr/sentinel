// Package policy wraps the Open Policy Agent (OPA) evaluation engine.
//
// Policy bundles are loaded from a remote URL or local path and refreshed
// automatically. Every packet evaluation produces a DecisionRecord that
// includes the bundle revision, decision ID, and masking profile applied.
//
// The engine supports three modes:
//   - observe: evaluate but never block; all decisions are advisory.
//   - guard:   deny severe violations; allow low-risk events after local witness.
//   - enforce: high-risk actions require chain or witness acknowledgement.
package policy

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/rego"
	"github.com/your-org/sentinel/internal/core"
	"go.uber.org/zap"
)

// Engine evaluates governance packets against an OPA policy bundle.
type Engine struct {
	bundleURL  string
	bundleID   string
	bundleHash string
	log        *zap.Logger
	query      *rego.PreparedEvalQuery
}

// NewEngine creates and prepares an OPA query against the named policy.
// bundleURL may be a file:// path for local development or an https:// URL
// for production bundle distribution.
func NewEngine(ctx context.Context, bundleURL string, log *zap.Logger) (*Engine, error) {
	r := rego.New(
		rego.Query("data.sentinel.authz"),
		rego.LoadBundle(bundleURL),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("policy: prepare OPA query: %w", err)
	}

	return &Engine{
		bundleURL: bundleURL,
		log:       log,
		query:     &pq,
	}, nil
}

// EvaluateInput is the data object sent to OPA for each packet evaluation.
type EvaluateInput struct {
	Packet *core.Packet          `json:"packet"`
	App    *core.AppRegistration `json:"app"`
	Mode   core.SentinelMode     `json:"mode"`
}

// EvaluateResult is the structured response from OPA.
type EvaluateResult struct {
	Decision core.Decision `json:"decision"`
	Reason   string        `json:"reason"`
	BundleID string        `json:"bundle_id"`
}

// Evaluate runs a packet through the OPA policy and returns a decision.
func (e *Engine) Evaluate(ctx context.Context, input *EvaluateInput) (*EvaluateResult, error) {
	rs, err := e.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return nil, fmt.Errorf("policy: OPA eval: %w", err)
	}

	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return nil, fmt.Errorf("policy: OPA returned empty result set")
	}

	// The policy is expected to return a map with keys: decision, reason.
	result, ok := rs[0].Expressions[0].Value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("policy: unexpected OPA result shape")
	}

	decision := core.Decision(fmt.Sprintf("%v", result["decision"]))
	reason, _ := result["reason"].(string)

	return &EvaluateResult{
		Decision: decision,
		Reason:   reason,
		BundleID: e.bundleID,
	}, nil
}

// SimulationResult describes what a policy bundle would decide for a given packet.
type SimulationResult struct {
	Current  *EvaluateResult `json:"current"`
	Proposed *EvaluateResult `json:"proposed,omitempty"`
	Changed  bool            `json:"changed"`
}

// Simulate runs a packet against an alternative bundle to preview the impact
// before promotion. Used by sentinelctl simulate-policy.
func (e *Engine) Simulate(
	ctx context.Context,
	input *EvaluateInput,
	proposedBundleURL string,
) (*SimulationResult, error) {
	current, err := e.Evaluate(ctx, input)
	if err != nil {
		return nil, err
	}

	proposedEngine, err := NewEngine(ctx, proposedBundleURL, e.log)
	if err != nil {
		return nil, fmt.Errorf("policy: load proposed bundle: %w", err)
	}

	proposed, err := proposedEngine.Evaluate(ctx, input)
	if err != nil {
		return nil, err
	}

	return &SimulationResult{
		Current:  current,
		Proposed: proposed,
		Changed:  current.Decision != proposed.Decision,
	}, nil
}
