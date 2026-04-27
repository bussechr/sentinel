// Package identity — OIDC token identity provider.
//
// OIDC tokens are used for human operator and service account identity.
// The token is verified against the OIDC discovery document and the
// subject claim is hashed to produce the actor ID hash.
package identity

import (
	"context"
	"crypto/sha256"
	"fmt"
)

// OIDCProvider verifies OIDC JWTs and extracts the subject claim.
type OIDCProvider struct {
	IssuerURL string
	Audience  string
}

// NewOIDCProvider creates an OIDC provider for the given issuer.
func NewOIDCProvider(issuerURL, audience string) *OIDCProvider {
	return &OIDCProvider{IssuerURL: issuerURL, Audience: audience}
}

// VerifyAndHash validates the JWT token and returns the SHA-256 hash of the subject.
// The raw subject (e.g. email, user ID) is never persisted.
func (p *OIDCProvider) VerifyAndHash(ctx context.Context, rawToken string) (string, error) {
	// TODO: use github.com/coreos/go-oidc/v3 to verify the JWT.
	// Stub: hash the raw token for dev/test environments.
	if rawToken == "" {
		return "", fmt.Errorf("oidc: empty token")
	}
	h := sha256.Sum256([]byte(rawToken))
	return fmt.Sprintf("sha256:%x", h), nil
}
