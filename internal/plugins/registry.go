// Package plugins provides the out-of-process plugin registry.
//
// Plugins connect via gRPC or HTTP adapters rather than Go's standard plugin
// package. This avoids the portability and versioning limitations of the
// Go plugin system and allows plugins to be written in any language.
//
// Plugin registration: plugins declare their capabilities over gRPC at startup.
// Sentinel routes capture events to registered plugins based on category.
package plugins

// Registry tracks registered plugin connections.
type Registry struct {
	// TODO: implement gRPC plugin registration and routing.
}
