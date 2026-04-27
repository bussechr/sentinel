// Package network captures network egress events as governance packets.
//
// On Linux/Kubernetes: driven by Tetragon eBPF network events (M6).
// The adapter produces packets with category="network" for outbound connections
// to destinations not in the allowlist.
//
// M6 implementation milestone.
package network
