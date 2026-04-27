// Package policy — masking helpers.
//
// Masking redacts sensitive fields from OPA decision logs and packet records
// before they are stored or exported. The masking profile is declared per-app
// and per-route and is recorded on every decision.
//
// Built-in profiles:
//   default-dev  — no masking; all fields visible.
//   default-prod — masks actor ID hashes, tenant hashes, payload body hashes.
//   pii-strict   — additionally masks resource IDs and correlation IDs.
package policy

import "github.com/your-org/sentinel/internal/core"

// MaskingProfile defines which packet fields to redact.
type MaskingProfile struct {
	Name            string
	MaskActorIDHash bool
	MaskTenantHash  bool
	MaskBodyHash    bool
	MaskResourceID  bool
	MaskCorrID      bool
}

var builtinProfiles = map[string]*MaskingProfile{
	"default-dev": {
		Name: "default-dev",
	},
	"default-prod": {
		Name:            "default-prod",
		MaskActorIDHash: true,
		MaskTenantHash:  true,
		MaskBodyHash:    true,
	},
	"pii-strict": {
		Name:            "pii-strict",
		MaskActorIDHash: true,
		MaskTenantHash:  true,
		MaskBodyHash:    true,
		MaskResourceID:  true,
		MaskCorrID:      true,
	},
}

const masked = "redacted"

// Apply returns a copy of the packet with sensitive fields masked according
// to the named profile. If the profile is unknown, default-prod is used.
func Apply(p *core.Packet, profileName string) *core.Packet {
	profile, ok := builtinProfiles[profileName]
	if !ok {
		profile = builtinProfiles["default-prod"]
	}

	// Work on a shallow copy to avoid mutating the original.
	cp := *p
	actor := cp.Actor
	resource := cp.Resource
	payload := cp.Payload

	if profile.MaskActorIDHash {
		actor.IDHash = masked
	}
	if profile.MaskTenantHash {
		resource.TenantHash = masked
	}
	if profile.MaskBodyHash {
		payload.BodyHash = masked
	}
	if profile.MaskResourceID {
		resource.IDHash = masked
	}
	if profile.MaskCorrID {
		cp.CorrelationID = masked
	}

	cp.Actor = actor
	cp.Resource = resource
	cp.Payload = payload
	return &cp
}

// ProfileNames returns the list of built-in masking profile names.
func ProfileNames() []string {
	names := make([]string, 0, len(builtinProfiles))
	for k := range builtinProfiles {
		names = append(names, k)
	}
	return names
}
