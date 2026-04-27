// Package k8s captures Kubernetes deployment actions as governance packets.
//
// Sources:
//   - Kubernetes admission webhook (mutating or validating)
//   - kubectl audit logs
//   - Falco k8s rules
//
// All k8s packets use category="k8s" and risk is escalated to high by the
// risk classifier regardless of the declared risk.
//
// M6 implementation milestone.
package k8s
