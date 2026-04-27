// Package plugins — gRPC plugin adapter.
//
// The gRPC plugin adapter allows out-of-process plugins to register capture
// handlers with Sentinel over a gRPC connection. This avoids the portability
// issues of Go's native plugin package.
//
// Plugin lifecycle:
//   1. Plugin starts and connects to Sentinel plugin endpoint (default :9099).
//   2. Plugin calls Register RPC with its capabilities (capture categories).
//   3. Sentinel routes capture events to registered plugins based on category.
//   4. Plugin streams captured packets back to Sentinel via the Ingest RPC.
//
// M5 implementation milestone.
package plugins

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// Capability describes what a plugin can capture.
type Capability struct {
	PluginID   string
	Categories []string // "http", "grpc", "db", etc.
	Version    string
}

// GRPCAdapter manages out-of-process plugin connections.
type GRPCAdapter struct {
	mu          sync.RWMutex
	plugins     map[string]*Capability
	log         *zap.Logger
	listenAddr  string
}

// NewGRPCAdapter creates the gRPC plugin adapter.
func NewGRPCAdapter(listenAddr string, log *zap.Logger) *GRPCAdapter {
	return &GRPCAdapter{
		plugins:    make(map[string]*Capability),
		log:        log,
		listenAddr: listenAddr,
	}
}

// Register records a plugin's capabilities.
func (a *GRPCAdapter) Register(cap *Capability) error {
	if cap.PluginID == "" {
		return fmt.Errorf("plugin: empty plugin_id")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.plugins[cap.PluginID] = cap
	a.log.Info("plugin registered",
		zap.String("plugin_id", cap.PluginID),
		zap.Strings("categories", cap.Categories),
	)
	return nil
}

// Deregister removes a plugin.
func (a *GRPCAdapter) Deregister(pluginID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.plugins, pluginID)
	a.log.Info("plugin deregistered", zap.String("plugin_id", pluginID))
}

// PluginsForCategory returns all plugins that handle the given category.
func (a *GRPCAdapter) PluginsForCategory(category string) []*Capability {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []*Capability
	for _, p := range a.plugins {
		for _, c := range p.Categories {
			if c == category {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// Start begins listening for plugin connections.
// TODO M5: implement gRPC server with Register and Ingest RPCs.
func (a *GRPCAdapter) Start(ctx context.Context) error {
	a.log.Info("gRPC plugin adapter starting (stub)", zap.String("addr", a.listenAddr))
	<-ctx.Done()
	return nil
}
