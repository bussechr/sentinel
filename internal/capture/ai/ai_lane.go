// Package ai implements the AI traceability lane and the AI ingress gate.
//
// The AI lane captures:
//   - model call authorisation (before execution)
//   - prompt hash, model identity hash
//   - tool call count and call graph
//   - response hash (after execution)
//
// Every AI event is a governed packet with category="ai" or category="tool".
// Direct AI bypass — calls that do not flow through the AI gateway header
// contract — is blocked by Gate.Enforce.
package ai

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/your-org/sentinel/internal/core"
)

// HeaderActor is the header an AI client must set so the gate can verify
// that the request originated through the governed AI lane, not through
// a direct application path.
const HeaderActor = "X-Sentinel-AI-Actor"

// HeaderRoute is the AI lane gateway identifier. The gate checks both
// HeaderActor and HeaderRoute so direct app traffic with a hand-set
// actor header is still rejected.
const HeaderRoute = "X-Sentinel-AI-Route"

// HeaderRouteValue is the expected value of HeaderRoute. Any other value
// is treated as a missing header.
const HeaderRouteValue = "ai-gateway"

// ErrMissingAIRoute means a model/agent request did not flow through the
// AI gateway. Callers should map this to HTTP 403.
var ErrMissingAIRoute = errors.New("ai_lane: machine actor missing AI gateway route header")

// ErrMissingAIActor means the actor header was absent for a category=ai
// or category=tool packet.
var ErrMissingAIActor = errors.New("ai_lane: AI actor header required for AI/tool category")

// Gate enforces that machine actors enter through the AI gateway.
//
// The gate does not evaluate policy — it is a structural check that
// runs before the policy engine. Allowed actor types pass through;
// blocked actor types must carry the AI gateway header pair.
type Gate struct {
	// AllowedFromAnywhere is the set of actor types that may originate
	// requests without the AI gateway headers. Humans and services
	// always pass; agents and models must enter through the gateway.
	AllowedFromAnywhere map[core.ActorType]bool
}

// NewGate returns a gate with the default permitted actor set.
func NewGate() *Gate {
	return &Gate{
		AllowedFromAnywhere: map[core.ActorType]bool{
			core.ActorHuman:   true,
			core.ActorService: true,
			core.ActorSystem:  true,
		},
	}
}

// Verify checks that an HTTP request carrying the given Packet is
// allowed to reach the policy engine.
//
// Returns nil when the request passes the structural gate, or one of
// the ErrMissing* errors otherwise.
func (g *Gate) Verify(r *http.Request, p *core.Packet) error {
	if g == nil || p == nil {
		return nil
	}

	// AI/tool category packets must always carry the actor header,
	// regardless of who claims to have sent them.
	if p.Action.Category == core.CategoryAI || p.Action.Category == core.CategoryTool {
		if r.Header.Get(HeaderActor) == "" {
			return ErrMissingAIActor
		}
	}

	// Machine actors (agent/model) must additionally carry the route
	// header set by the AI gateway.
	if g.actorRequiresGateway(p.Actor.Type) {
		if !strings.EqualFold(r.Header.Get(HeaderRoute), HeaderRouteValue) {
			return ErrMissingAIRoute
		}
	}
	return nil
}

func (g *Gate) actorRequiresGateway(t core.ActorType) bool {
	if t == "" {
		return false
	}
	return !g.AllowedFromAnywhere[t]
}

// EnforceMiddleware is an http.Handler middleware that wraps a handler
// requiring the AI gateway route header. It is intended for use on the
// /v1/ai/* endpoints to refuse requests that did not transit the
// dedicated gateway.
func (g *Gate) EnforceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get(HeaderRoute), HeaderRouteValue) {
			http.Error(w, fmt.Sprintf("ai_lane: missing or invalid %s header", HeaderRoute), http.StatusForbidden)
			return
		}
		if r.Header.Get(HeaderActor) == "" {
			http.Error(w, fmt.Sprintf("ai_lane: %s header is required", HeaderActor), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
