// Package sentinel is the Go SDK for connecting applications to Sentinel.
//
// Minimal integration (observe mode):
//
//	client := sentinel.NewClient(sentinel.Config{
//	    Endpoint: "https://sentinel-api.internal:8443",
//	    AppID:    "billing-api",
//	    Mode:     sentinel.ModeGuard,
//	})
//	mux.Handle("/refund", client.HTTPMiddleware(
//	    sentinel.RoutePolicy{
//	        ActionName: "invoice.refund.create",
//	        Category:   "http",
//	        Risk:       "high",
//	        Mutating:   true,
//	    },
//	    handler,
//	))
package sentinel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Mode is the operational mode for a connected application.
type Mode string

const (
	ModeObserve Mode = "observe"
	ModeGuard   Mode = "guard"
	ModeEnforce Mode = "enforce"
)

// Config holds the SDK client configuration.
type Config struct {
	Endpoint string
	AppID    string
	Mode     Mode
	Token    string        // API token; prefer SPIFFE SVID in production
	Timeout  time.Duration // default: 5s
}

// Client is a thread-safe Sentinel SDK client.
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient creates a Sentinel client with the provided configuration.
func NewClient(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: cfg.Timeout},
	}
}

// RoutePolicy describes the governance policy for one HTTP route.
type RoutePolicy struct {
	ActionName   string // e.g. "invoice.refund.create"
	Category     string // "http", "grpc", "db", etc.
	Risk         string // "low", "medium", "high", "critical"
	Mutating     bool
	ResourceType string
}

// AuthorizeRequest is the packet sent to POST /v1/packets/authorize.
type AuthorizeRequest struct {
	AppID         string `json:"app_id"`
	ActorType     string `json:"actor_type"`
	ActionName    string `json:"action_name"`
	Category      string `json:"category"`
	Risk          string `json:"risk"`
	Mutating      bool   `json:"mutating"`
	ResourceType  string `json:"resource_type,omitempty"`
	PayloadHash   string `json:"payload_hash"`
	CorrelationID string `json:"correlation_id,omitempty"`
	TraceID       string `json:"trace_id,omitempty"`
}

// AuthorizeResponse is the response from POST /v1/packets/authorize.
type AuthorizeResponse struct {
	Decision       string `json:"decision"`
	DecisionID     string `json:"decision_id"`
	PacketID       string `json:"packet_id"`
	LedgerRequired bool   `json:"ledger_required"`
	ReceiptStatus  string `json:"receipt_status"`
}

// Authorize sends an authorization request to Sentinel and returns the decision.
// In observe mode, all decisions are advisory. In guard mode, deny decisions
// should block the action. In enforce mode, allow must be received before proceeding.
func (c *Client) Authorize(ctx context.Context, correlationID string, policy RoutePolicy, bodyHash string) (*AuthorizeResponse, error) {
	req := AuthorizeRequest{
		AppID:         c.cfg.AppID,
		ActorType:     "service",
		ActionName:    policy.ActionName,
		Category:      policy.Category,
		Risk:          policy.Risk,
		Mutating:      policy.Mutating,
		ResourceType:  policy.ResourceType,
		PayloadHash:   bodyHash,
		CorrelationID: correlationID,
	}

	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("sentinel sdk: marshal authorize request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint+"/v1/packets/authorize", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("sentinel sdk: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// Fail open in observe mode; fail closed in guard/enforce.
		if c.cfg.Mode == ModeObserve {
			return &AuthorizeResponse{Decision: "allow", DecisionID: "degraded"}, nil
		}
		return nil, fmt.Errorf("sentinel sdk: authorize request failed: %w", err)
	}
	defer resp.Body.Close()

	var authResp AuthorizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("sentinel sdk: decode response: %w", err)
	}
	return &authResp, nil
}

// HTTPMiddleware wraps an http.Handler with Sentinel packet evaluation.
// It extracts or generates a correlation ID, hashes the request body,
// calls Authorize, and either proceeds or returns 403 depending on mode and decision.
func (c *Client) HTTPMiddleware(policy RoutePolicy, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := r.Header.Get("X-Correlation-ID")
		if correlationID == "" {
			correlationID = "corr_" + uuid.New().String()
		}

		// Read and hash the body (if present).
		var bodyHash string
		if r.Body != nil && r.Body != http.NoBody {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
			if err == nil {
				sum := sha256.Sum256(body)
				bodyHash = fmt.Sprintf("sha256:%x", sum)
				r.Body = io.NopCloser(bytes.NewReader(body))
			}
		}
		if bodyHash == "" {
			bodyHash = "sha256:" + fmt.Sprintf("%x", sha256.Sum256(nil))
		}

		decision, err := c.Authorize(r.Context(), correlationID, policy, bodyHash)
		if err != nil {
			if c.cfg.Mode != ModeObserve {
				http.Error(w, `{"error":"sentinel authorization failed"}`, http.StatusForbidden)
				return
			}
			// observe mode: log but continue.
		}

		if decision != nil && decision.Decision == "deny" && c.cfg.Mode != ModeObserve {
			w.Header().Set("X-Sentinel-Decision-ID", decision.DecisionID)
			w.Header().Set("X-Sentinel-Packet-ID", decision.PacketID)
			http.Error(w, `{"error":"denied by sentinel policy"}`, http.StatusForbidden)
			return
		}

		if decision != nil {
			w.Header().Set("X-Sentinel-Decision", decision.Decision)
			w.Header().Set("X-Sentinel-Packet-ID", decision.PacketID)
		}
		w.Header().Set("X-Correlation-ID", correlationID)

		next.ServeHTTP(w, r.WithContext(
			context.WithValue(r.Context(), correlationIDKey{}, correlationID),
		))
	})
}

// correlationIDKey is the context key for the correlation ID.
type correlationIDKey struct{}

// CorrelationIDFromContext retrieves the correlation ID from the context.
func CorrelationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(correlationIDKey{}).(string)
	return v
}
