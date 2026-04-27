// Package file captures filesystem access events as governance packets.
//
// On Linux/Kubernetes: driven by Tetragon eBPF file events (M6).
// On all platforms: inotify/FSEvents watcher for declared sensitive paths.
//
// Sensitive paths are declared in the app registration and matched against
// the file access events. Non-matching paths are not captured.
//
// M6 implementation milestone.
package file
