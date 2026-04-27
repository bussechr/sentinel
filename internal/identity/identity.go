// Package identity provides workload identity resolution.
//
// Supported providers:
//   - SPIFFE/SPIRE: SVIDs let workloads present cryptographically verifiable identities.
//   - OIDC: JWT-based identity for human operators and service accounts.
//   - API keys: simple token-based identity for legacy integrations.
//   - Local: development-mode identity with no verification.
//
// Identity resolution populates the Actor.IDHash field on every packet.
// The raw identity is never stored; only the SHA-256 hash is persisted.
package identity
