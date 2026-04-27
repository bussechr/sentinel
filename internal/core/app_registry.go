// Package core — application registry types.
//
// Every application that connects to Sentinel is first registered.
// The registry record controls which packet categories are permitted,
// what policy scope is applied, and which signing key the app uses.
package core

import "time"

// SentinelMode is the operational mode for an application.
type SentinelMode string

const (
	ModeObserve SentinelMode = "observe"
	ModeGuard   SentinelMode = "guard"
	ModeEnforce SentinelMode = "enforce"
)

// AppRegistration records a connected application's identity and configuration.
type AppRegistration struct {
	AppID                string         `json:"app_id" db:"app_id"`
	Service              string         `json:"service" db:"service"`
	Environment          string         `json:"environment" db:"environment"`
	Owner                string         `json:"owner" db:"owner"`
	Mode                 SentinelMode   `json:"mode" db:"mode"`
	RiskTier             RiskLevel      `json:"risk_tier" db:"risk_tier"`
	AllowedCategories    []ActionCategory `json:"allowed_categories" db:"allowed_categories"`
	PolicyScope          string         `json:"policy_scope" db:"policy_scope"`
	SigningKeyRef        string         `json:"signing_key_ref" db:"signing_key_ref"`
	RegistrationToken    string         `json:"-" db:"registration_token"` // never serialised out
	RegisteredAt         time.Time      `json:"registered_at" db:"registered_at"`
	LastSeenAt           *time.Time     `json:"last_seen_at,omitempty" db:"last_seen_at"`
}
