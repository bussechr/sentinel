// Package ai implements the AI traceability lane.
//
// The AI lane captures:
//   - model call authorisation (before execution)
//   - prompt hash, model identity hash
//   - tool call count and call graph
//   - response hash (after execution)
//
// Every AI event is a governed packet with category="ai" or category="tool".
// Direct AI bypass (calls that do not flow through Sentinel) is blocked by policy.
//
// M5 implementation milestone.
package ai
