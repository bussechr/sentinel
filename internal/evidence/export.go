// Package evidence — evidence export with signed manifest.
//
// An export bundles the evidence segments for a given correlation ID,
// applies a redaction profile, and produces a signed manifest.
// No raw export is permitted without a declared redaction profile.
// The manifest is signed with the Sentinel API signing key so that
// the recipient can verify authenticity without trusting the transport.
package evidence

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ExportManifest describes a signed evidence bundle.
type ExportManifest struct {
	ManifestID       string    `json:"manifest_id"`
	CorrelationID    string    `json:"correlation_id"`
	AppID            string    `json:"app_id"`
	RedactionProfile string    `json:"redaction_profile"`
	WindowFrom       time.Time `json:"window_from"`
	WindowTo         time.Time `json:"window_to"`
	SegmentIDs       []string  `json:"segment_ids"`
	ManifestHash     string    `json:"manifest_hash"`
	Signature        string    `json:"signature"` // ed25519:<hex>
	PublicKey        string    `json:"public_key"`
	ExportedAt       time.Time `json:"exported_at"`
}

// BuildManifest creates and signs an export manifest.
// privKey is the Sentinel API signing key (ed25519), loaded from a secret.
func BuildManifest(
	correlationID, appID, redactionProfile string,
	from, to time.Time,
	segmentIDs []string,
	privKey ed25519.PrivateKey,
) (*ExportManifest, error) {
	if redactionProfile == "" {
		return nil, fmt.Errorf("export: redaction_profile is required")
	}

	id := "exp_" + uuid.New().String()
	now := time.Now().UTC()

	m := &ExportManifest{
		ManifestID:       id,
		CorrelationID:    correlationID,
		AppID:            appID,
		RedactionProfile: redactionProfile,
		WindowFrom:       from,
		WindowTo:         to,
		SegmentIDs:       segmentIDs,
		ExportedAt:       now,
		PublicKey:        hex.EncodeToString(privKey.Public().(ed25519.PublicKey)),
	}

	// Hash the manifest body before signing.
	body, err := json.Marshal(struct {
		ManifestID    string    `json:"manifest_id"`
		CorrelationID string    `json:"correlation_id"`
		SegmentIDs    []string  `json:"segment_ids"`
		ExportedAt    time.Time `json:"exported_at"`
	}{m.ManifestID, m.CorrelationID, m.SegmentIDs, m.ExportedAt})
	if err != nil {
		return nil, fmt.Errorf("export: marshal for hash: %w", err)
	}

	sum := sha256.Sum256(body)
	m.ManifestHash = fmt.Sprintf("sha256:%x", sum)

	sig := ed25519.Sign(privKey, []byte(m.ManifestHash))
	m.Signature = "ed25519:" + hex.EncodeToString(sig)

	return m, nil
}
