// Package policy — risk classification helpers.
//
// Risk is derived from two sources:
//  1. The emitting app declares a risk level on each action.
//  2. Sentinel can override or escalate risk based on internal rules
//     (mutating + critical resource type, AI tool call, secret access, etc.)
//
// The final risk is stored on the Packet before policy evaluation.
package policy

import "github.com/your-org/sentinel/internal/core"

// RiskOverrideRule escalates risk when the condition is met.
type RiskOverrideRule struct {
	Name      string
	Condition func(p *core.Packet) bool
	MinRisk   core.RiskLevel
}

// defaultRules is the built-in set of escalation rules.
var defaultRules = []RiskOverrideRule{
	{
		Name: "secret-access-always-high",
		Condition: func(p *core.Packet) bool {
			return p.Action.Category == core.CategorySecret
		},
		MinRisk: core.RiskHigh,
	},
	{
		Name: "ai-tool-call-minimum-medium",
		Condition: func(p *core.Packet) bool {
			return p.AI.IsAIRelated && p.AI.ToolCallCount > 0
		},
		MinRisk: core.RiskMedium,
	},
	{
		Name: "mutating-critical-resource",
		Condition: func(p *core.Packet) bool {
			return p.Action.Mutating && p.Resource.Type == "customer_record"
		},
		MinRisk: core.RiskHigh,
	},
	{
		Name: "k8s-deployment-action",
		Condition: func(p *core.Packet) bool {
			return p.Action.Category == core.CategoryK8s
		},
		MinRisk: core.RiskHigh,
	},
}

// riskOrder maps a RiskLevel to a comparable integer.
var riskOrder = map[core.RiskLevel]int{
	core.RiskLow:      1,
	core.RiskMedium:   2,
	core.RiskHigh:     3,
	core.RiskCritical: 4,
}

// Classify applies override rules to a packet and returns the effective risk.
// The declared risk is never lowered; it can only be escalated.
func Classify(p *core.Packet, extraRules ...RiskOverrideRule) core.RiskLevel {
	effective := p.Action.Risk
	rules := append(defaultRules, extraRules...) //nolint:gocritic
	for _, rule := range rules {
		if rule.Condition(p) {
			if riskOrder[rule.MinRisk] > riskOrder[effective] {
				effective = rule.MinRisk
			}
		}
	}
	return effective
}
