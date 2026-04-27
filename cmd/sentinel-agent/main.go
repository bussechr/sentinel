// sentinel-agent collects runtime, kernel, and application signals from the host
// or container node and forwards them to sentinel-api as governed packets.
//
// In Kubernetes the agent runs as a DaemonSet (one pod per node).
// On bare metal or Docker it runs as a host service.
//
// Capture adapters (enabled via config):
//   - Tetragon gRPC stream (eBPF kernel events)
//   - journald log tail
//   - Host process/file/network events
//
// All captured events are normalised into the sentinel.packet.v1 format
// before being sent to the sentinel-api ingestion endpoint.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

func main() {
	log, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sentinel-agent: failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync() //nolint:errcheck

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	apiEndpoint := os.Getenv("SENTINEL_API_ENDPOINT")
	if apiEndpoint == "" {
		apiEndpoint = "http://sentinel-api:8080"
	}
	nodeID := os.Getenv("SENTINEL_NODE_ID")
	if nodeID == "" {
		nodeID, _ = os.Hostname()
	}

	log.Info("sentinel-agent starting",
		zap.String("api_endpoint", apiEndpoint),
		zap.String("node_id", nodeID),
	)

	// TODO M6: initialise Tetragon gRPC stream adapter.
	// TODO M6: initialise journald tail adapter.
	// TODO M6: start heartbeat loop and emit degraded status when adapters disconnect.

	// Heartbeat ticker — reports agent liveness to sentinel-api.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("sentinel-agent shutting down")
			return
		case <-ticker.C:
			log.Info("agent heartbeat", zap.String("node_id", nodeID))
			// TODO: POST heartbeat to /v1/agents/{node_id}/heartbeat
		}
	}
}
