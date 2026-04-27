// Package core defines the canonical governance packet schema (sentinel.packet.v1)
// and shared value types used across all Sentinel components.
//
// Every integration path — HTTP proxy, SDK middleware, sidecar, gRPC adapter,
// runtime agent — emits the same Packet. That shared contract is the Sentinel moat.
package core

import "time"

// SchemaVersion is the packet schema identifier burned into every packet.
const SchemaVersion = "sentinel.packet.v1"

// ActorType enumerates the kinds of principal that can originate a packet.
type ActorType string

const (
	ActorHuman   ActorType = "human"
	ActorService ActorType = "service"
	ActorAgent   ActorType = "agent"
	ActorModel   ActorType = "model"
	ActorSystem  ActorType = "system"
)

// ActionCategory classifies the governed action.
type ActionCategory string

const (
	CategoryHTTP    ActionCategory = "http"
	CategoryGRPC    ActionCategory = "grpc"
	CategoryAI      ActionCategory = "ai"
	CategoryTool    ActionCategory = "tool"
	CategoryDB      ActionCategory = "db"
	CategoryFile    ActionCategory = "file"
	CategoryNetwork ActionCategory = "network"
	CategoryK8s     ActionCategory = "k8s"
	CategorySecret  ActionCategory = "secret"
	CategoryConfig  ActionCategory = "config"
)

// RiskLevel describes the assessed risk of an action.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// Decision is the outcome of a policy evaluation.
type Decision string

const (
	DecisionAllow    Decision = "allow"
	DecisionWarn     Decision = "warn"
	DecisionDeny     Decision = "deny"
	DecisionEscalate Decision = "escalate"
)

// AnchorMode controls when a packet hash is submitted to the governance chain.
type AnchorMode string

const (
	AnchorBatch     AnchorMode = "batch"
	AnchorImmediate AnchorMode = "immediate"
	AnchorRequired  AnchorMode = "required"
)

// AnchorStatus tracks chain anchoring progress.
type AnchorStatus string

const (
	AnchorQueued   AnchorStatus = "queued"
	AnchorAccepted AnchorStatus = "accepted"
	AnchorAnchored AnchorStatus = "anchored"
	AnchorFailed   AnchorStatus = "failed"
)

// IdentityProvider classifies how an actor's identity was verified.
type IdentityProvider string

const (
	IDProviderOIDC   IdentityProvider = "oidc"
	IDProviderSPIFFE IdentityProvider = "spiffe"
	IDProviderAPIKey IdentityProvider = "api_key"
	IDProviderLocal  IdentityProvider = "local"
)

// ----- Packet sub-structs -----

// AppContext identifies the application and environment that produced the packet.
type AppContext struct {
	AppID       string `json:"app_id"`
	Service     string `json:"service"`
	Environment string `json:"environment"`
	Version     string `json:"version"`
}

// Actor describes the principal that initiated the governed action.
// Sensitive identifiers are stored as SHA-256 hashes.
type Actor struct {
	Type             ActorType        `json:"type"`
	IDHash           string           `json:"id_hash"`
	IdentityProvider IdentityProvider `json:"identity_provider"`
}

// Action describes what happened.
type Action struct {
	Category ActionCategory `json:"category"`
	Name     string         `json:"name"`
	Risk     RiskLevel      `json:"risk"`
	Mutating bool           `json:"mutating"`
}

// Resource is the governed object the action targeted.
// IDs and tenant identifiers are hashed to avoid storing PII in the hot index.
type Resource struct {
	Type       string `json:"type"`
	IDHash     string `json:"id_hash"`
	TenantHash string `json:"tenant_hash"`
}

// Payload describes where the full request body is stored and how it was redacted.
type Payload struct {
	BodyHash         string `json:"body_hash"`
	RedactionProfile string `json:"redaction_profile"`
	ObjectURI        string `json:"object_uri,omitempty"`
}

// PolicyRecord captures the policy evaluation result attached to this packet.
type PolicyRecord struct {
	BundleID   string   `json:"bundle_id"`
	DecisionID string   `json:"decision_id"`
	Decision   Decision `json:"decision"`
	Reason     string   `json:"reason"`
}

// LedgerRecord tracks the governance-chain anchoring state.
type LedgerRecord struct {
	AnchorMode   AnchorMode   `json:"anchor_mode"`
	AnchorStatus AnchorStatus `json:"anchor_status"`
	ReceiptID    string       `json:"receipt_id,omitempty"`
}

// AIRecord holds AI-specific traceability fields. Present only when IsAIRelated is true.
type AIRecord struct {
	IsAIRelated   bool   `json:"is_ai_related"`
	ModelIDHash   string `json:"model_id_hash,omitempty"`
	PromptHash    string `json:"prompt_hash,omitempty"`
	ResponseHash  string `json:"response_hash,omitempty"`
	ToolCallCount int    `json:"tool_call_count,omitempty"`
}

// ----- Root packet -----

// Packet is the canonical governance envelope (sentinel.packet.v1).
//
// Every integration path — middleware, proxy, agent, SDK — produces a Packet.
// The schema is stable across app types; only the Action.Category field changes.
type Packet struct {
	SchemaVersion string    `json:"schema_version"`
	PacketID      string    `json:"packet_id"`
	CorrelationID string    `json:"correlation_id"`
	TraceID       string    `json:"trace_id,omitempty"`
	CapturedAt    time.Time `json:"captured_at"`

	App      AppContext    `json:"app"`
	Actor    Actor        `json:"actor"`
	Action   Action       `json:"action"`
	Resource Resource     `json:"resource"`
	Payload  Payload      `json:"payload"`
	Policy   PolicyRecord `json:"policy"`
	Ledger   LedgerRecord `json:"ledger"`
	AI       AIRecord     `json:"ai,omitempty"`
}
