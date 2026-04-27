// Package identity — SPIFFE/SPIRE workload identity.
//
// SPIFFE SVIDs give workloads cryptographically verifiable identities without
// relying on API tokens. Sentinel uses SPIFFE SVIDs for service-to-service
// mTLS and for actor identity proofs in high-risk packets.
//
// In production: mount the SPIRE agent Unix socket into the container and set
// SPIFFE_ENDPOINT_SOCKET=unix:///run/spire/sockets/agent.sock
//
// Reference: https://spiffe.io/docs/latest/spiffe-specs/spiffe-id/
package identity

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
)

// SPIFFEProvider resolves workload identity via the SPIRE agent socket.
type SPIFFEProvider struct {
	socketPath string
}

// NewSPIFFEProvider creates a SPIFFE identity provider.
// socketPath defaults to the standard SPIRE agent socket if empty.
func NewSPIFFEProvider(socketPath string) *SPIFFEProvider {
	if socketPath == "" {
		if s := os.Getenv("SPIFFE_ENDPOINT_SOCKET"); s != "" {
			socketPath = s
		} else {
			socketPath = "unix:///run/spire/sockets/agent.sock"
		}
	}
	return &SPIFFEProvider{socketPath: socketPath}
}

// ResolveIDHash fetches the calling workload's SPIFFE ID and returns its SHA-256 hash.
// The raw SPIFFE ID is never persisted; only the hash reaches the Packet.
func (p *SPIFFEProvider) ResolveIDHash(ctx context.Context) (string, error) {
	// TODO: use github.com/spiffe/go-spiffe/v2 to fetch SVID from the agent.
	// Stub returns a deterministic hash of the socket path for dev mode.
	h := sha256.Sum256([]byte(p.socketPath))
	return fmt.Sprintf("sha256:%x", h), nil
}
