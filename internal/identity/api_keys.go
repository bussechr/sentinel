// Package identity — API key identity provider.
//
// API keys are used by legacy integrations that cannot present SPIFFE SVIDs
// or OIDC tokens. Each API key is associated with an app_id and a scope.
// The raw key is never stored; only its SHA-256 hash is persisted.
//
// Key rotation: issue a new key, update the app registration signing_key_ref,
// and revoke the old key. Both keys may coexist during the rotation window.
package identity

import (
	"context"
	"crypto/sha256"
	"fmt"
)

// APIKeyProvider validates API keys and resolves their app association.
type APIKeyProvider struct {
	// keys maps sha256-hashed API key → app_id.
	// In production, this is backed by Postgres.
	keys map[string]string
}

// NewAPIKeyProvider creates an in-memory API key provider (dev/test).
// Production: replace with a Postgres-backed implementation.
func NewAPIKeyProvider() *APIKeyProvider {
	return &APIKeyProvider{keys: make(map[string]string)}
}

// Register adds an API key → app_id mapping.
// The raw key is hashed immediately and discarded.
func (p *APIKeyProvider) Register(rawKey, appID string) {
	h := sha256.Sum256([]byte(rawKey))
	p.keys[fmt.Sprintf("sha256:%x", h)] = appID
}

// ResolveAppID validates the raw API key and returns the associated app_id and key hash.
func (p *APIKeyProvider) ResolveAppID(ctx context.Context, rawKey string) (appID, keyHash string, err error) {
	h := sha256.Sum256([]byte(rawKey))
	hash := fmt.Sprintf("sha256:%x", h)
	id, ok := p.keys[hash]
	if !ok {
		return "", "", fmt.Errorf("api_key: unknown or revoked key")
	}
	return id, hash, nil
}
