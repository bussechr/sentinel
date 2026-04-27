// Package tetragon adapts the Tetragon eBPF event stream into governance packets.
//
// Tetragon captures process execution, syscall activity, network and file access
// directly in the kernel via eBPF, reducing user-space event overhead.
// The adapter connects to Tetragon's gRPC API and maps each event to the
// appropriate packet category (file, network, k8s).
//
// M6 implementation milestone.
// Requires: Linux with eBPF support, Tetragon daemonset running on the node.
package tetragon
