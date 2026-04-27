// Package api implements all Sentinel HTTP handlers (v1).
//
// Handler wiring:
//   POST /v1/apps/register         → RegisterApp
//   POST /v1/packets               → IngestPacket
//   POST /v1/packets/authorize     → AuthorizePacket
//   GET  /v1/packets/{packet_id}   → GetPacket
//
//   POST /v1/ai/trace              → TraceAI
//   POST /v1/ai/authorize          → AuthorizeAI
//   POST /v1/ai/result             → RecordAIResult
//
//   POST /v1/ledger/anchor                      → AnchorPacket
//   GET  /v1/ledger/receipts/{packet_id}        → GetReceipt
//   GET  /v1/ledger/verify/{receipt_id}         → VerifyReceipt
//
//   GET  /v1/evidence/window                    → QueryWindow
//   POST /v1/evidence/export                    → ExportEvidence
//   GET  /v1/evidence/rewind/{correlation_id}   → RewindEvidence
//
//   GET  /v1/policy/bundles        → ListBundles
//   POST /v1/policy/simulate       → SimulatePolicy
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/evidence"
	"github.com/your-org/sentinel/internal/ledger"
	"github.com/your-org/sentinel/internal/policy"
	pgstore "github.com/your-org/sentinel/internal/store/postgres"
	"go.uber.org/zap"
)

// Handler holds the dependencies shared across all API handlers.
type Handler struct {
	store   *pgstore.Store
	policy  *policy.Engine
	queue   *ledger.Queue
	witness *ledger.Witness
	mode    core.SentinelMode
	log     *zap.Logger
}

// NewHandler creates the API handler bundle.
func NewHandler(
	store *pgstore.Store,
	policyEngine *policy.Engine,
	queue *ledger.Queue,
	witness *ledger.Witness,
	mode core.SentinelMode,
	log *zap.Logger,
) *Handler {
	return &Handler{
		store:   store,
		policy:  policyEngine,
		queue:   queue,
		witness: witness,
		mode:    mode,
		log:     log,
	}
}

// Register mounts all API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/v1/apps/register", h.RegisterApp)
	mux.HandleFunc("/v1/packets", h.IngestPacket)
	mux.HandleFunc("/v1/packets/authorize", h.AuthorizePacket)
	mux.HandleFunc("/v1/packets/", h.GetPacket) // GET /v1/packets/{id}

	mux.HandleFunc("/v1/ai/trace", h.TraceAI)
	mux.HandleFunc("/v1/ai/authorize", h.AuthorizeAI)
	mux.HandleFunc("/v1/ai/result", h.RecordAIResult)

	mux.HandleFunc("/v1/ledger/anchor", h.AnchorPacket)
	mux.HandleFunc("/v1/ledger/receipts/", h.GetReceipt)
	mux.HandleFunc("/v1/ledger/verify/", h.VerifyReceipt)

	mux.HandleFunc("/v1/evidence/window", h.QueryWindow)
	mux.HandleFunc("/v1/evidence/export", h.ExportEvidence)
	mux.HandleFunc("/v1/evidence/rewind/", h.RewindEvidence)

	mux.HandleFunc("/v1/policy/bundles", h.ListBundles)
	mux.HandleFunc("/v1/policy/simulate", h.SimulatePolicy)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, dst interface{}) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func pathSuffix(r *http.Request, prefix string) string {
	return strings.TrimPrefix(r.URL.Path, prefix)
}

func newID(prefix string) string {
	return prefix + uuid.New().String()
}

// ─── App registration ─────────────────────────────────────────────────────────

type registerAppRequest struct {
	AppID             string               `json:"app_id"`
	Service           string               `json:"service"`
	Environment       string               `json:"environment"`
	Owner             string               `json:"owner"`
	Mode              core.SentinelMode    `json:"mode"`
	RiskTier          core.RiskLevel       `json:"risk_tier"`
	AllowedCategories []core.ActionCategory `json:"allowed_categories"`
	PolicyScope       string               `json:"policy_scope"`
}

// RegisterApp handles POST /v1/apps/register.
func (h *Handler) RegisterApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req registerAppRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.AppID == "" {
		apiError(w, http.StatusBadRequest, "app_id is required")
		return
	}
	if req.Mode == "" {
		req.Mode = core.ModeObserve
	}
	if req.RiskTier == "" {
		req.RiskTier = core.RiskLow
	}
	if req.PolicyScope == "" {
		req.PolicyScope = "default"
	}

	token := newID("tok_")
	reg := &core.AppRegistration{
		AppID:             req.AppID,
		Service:           req.Service,
		Environment:       req.Environment,
		Owner:             req.Owner,
		Mode:              req.Mode,
		RiskTier:          req.RiskTier,
		AllowedCategories: req.AllowedCategories,
		PolicyScope:       req.PolicyScope,
		SigningKeyRef:      "key-ref:" + req.AppID,
		RegistrationToken: token,
		RegisteredAt:      time.Now().UTC(),
	}

	if err := h.store.RegisterApp(r.Context(), reg); err != nil {
		h.log.Error("register app", zap.Error(err))
		apiError(w, http.StatusInternalServerError, "failed to register app")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"app_id":          reg.AppID,
		"signing_key_ref": reg.SigningKeyRef,
		"token":           token, // shown once
		"registered_at":   reg.RegisteredAt,
	})
}

// ─── Packet intake ────────────────────────────────────────────────────────────

// IngestPacket handles POST /v1/packets (fire-and-forget).
func (h *Handler) IngestPacket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var p core.Packet
	if err := readJSON(r, &p); err != nil {
		apiError(w, http.StatusBadRequest, "invalid packet: "+err.Error())
		return
	}
	if p.PacketID == "" {
		p.PacketID = newID("pkt_")
	}
	if p.CorrelationID == "" {
		p.CorrelationID = newID("corr_")
	}
	if p.CapturedAt.IsZero() {
		p.CapturedAt = time.Now().UTC()
	}
	p.SchemaVersion = core.SchemaVersion

	// Evaluate policy.
	decision, err := h.evaluatePolicy(r, &p)
	if err != nil {
		h.log.Warn("policy eval failed (degraded)", zap.Error(err))
		if h.mode != core.ModeObserve && isFailClosed(p.Action.Risk) {
			apiError(w, http.StatusServiceUnavailable, "policy unavailable; action fail-closed")
			return
		}
	}
	if decision != nil {
		p.Policy.Decision = decision.Decision
		p.Policy.Reason = decision.Reason
		p.Policy.BundleID = decision.BundleID
	}

	if h.store != nil {
		if err := h.store.InsertPacket(r.Context(), &p); err != nil {
			h.log.Error("insert packet", zap.Error(err))
			apiError(w, http.StatusInternalServerError, "failed to store packet")
			return
		}
	}

	// Enqueue for chain anchoring.
	h.enqueueAnchor(r, &p)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"packet_id": p.PacketID,
		"status":    "accepted",
	})
}

// AuthorizePacket handles POST /v1/packets/authorize (synchronous decision).
func (h *Handler) AuthorizePacket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	type authorizeRequest struct {
		AppID         string               `json:"app_id"`
		ActorType     core.ActorType       `json:"actor_type"`
		ActorIDHash   string               `json:"actor_id_hash"`
		ActionName    string               `json:"action_name"`
		Category      core.ActionCategory  `json:"category"`
		Risk          core.RiskLevel       `json:"risk"`
		Mutating      bool                 `json:"mutating"`
		ResourceType  string               `json:"resource_type"`
		PayloadHash   string               `json:"payload_hash"`
		CorrelationID string               `json:"correlation_id"`
		TraceID       string               `json:"trace_id"`
	}

	var req authorizeRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.AppID == "" || req.ActionName == "" {
		apiError(w, http.StatusBadRequest, "app_id and action_name are required")
		return
	}

	// Build a packet from the authorize request.
	now := time.Now().UTC()
	p := &core.Packet{
		SchemaVersion: core.SchemaVersion,
		PacketID:      newID("pkt_"),
		CorrelationID: req.CorrelationID,
		TraceID:       req.TraceID,
		CapturedAt:    now,
		App:           core.AppContext{AppID: req.AppID},
		Actor:         core.Actor{Type: req.ActorType, IDHash: req.ActorIDHash, IdentityProvider: core.IDProviderLocal},
		Action:        core.Action{Category: req.Category, Name: req.ActionName, Risk: req.Risk, Mutating: req.Mutating},
		Resource:      core.Resource{Type: req.ResourceType},
		Payload:       core.Payload{BodyHash: req.PayloadHash, RedactionProfile: "default"},
	}
	if p.CorrelationID == "" {
		p.CorrelationID = newID("corr_")
	}

	// Apply risk escalation.
	p.Action.Risk = policy.Classify(p)

	// Evaluate policy.
	decision, err := h.evaluatePolicy(r, p)
	if err != nil {
		if h.mode != core.ModeObserve && isFailClosed(p.Action.Risk) {
			apiError(w, http.StatusServiceUnavailable, "policy unavailable; action fail-closed")
			return
		}
		decision = &policy.EvaluateResult{Decision: core.DecisionAllow, Reason: "policy degraded — observe fallback"}
	}
	if decision == nil {
		decision = &policy.EvaluateResult{Decision: core.DecisionAllow, Reason: "policy not configured"}
	}

	decisionID := newID("dec_")
	p.Policy = core.PolicyRecord{
		DecisionID: decisionID,
		Decision:   decision.Decision,
		Reason:     decision.Reason,
		BundleID:   decision.BundleID,
	}

	if h.store != nil {
		if err := h.store.InsertPacket(r.Context(), p); err != nil {
			h.log.Error("insert authorize packet", zap.Error(err))
		}
	}

	// Issue witness receipt.
	pktHash, _ := ledger.HashPacket(p)
	var wReceipt *ledger.WitnessReceipt
	if h.witness != nil {
		wReceipt, _ = h.witness.Issue(p.PacketID, pktHash, decision.Decision, p.Action.Risk)
	}

	// Enqueue anchor.
	receipt := h.enqueueAnchor(r, p)

	// Block if deny and not observe mode.
	if decision.Decision == core.DecisionDeny && h.mode != core.ModeObserve {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"decision":       decision.Decision,
			"decision_id":    decisionID,
			"packet_id":      p.PacketID,
			"reason":         decision.Reason,
			"ledger_required": false,
			"receipt_status": "denied",
			"witness_id":     wReceipt.WitnessID,
		})
		return
	}

	resp := map[string]interface{}{
		"decision":        decision.Decision,
		"decision_id":     decisionID,
		"packet_id":       p.PacketID,
		"reason":          decision.Reason,
		"ledger_required": isFailClosed(p.Action.Risk),
		"receipt_status":  "accepted",
	}
	if receipt != nil {
		resp["receipt_id"] = receipt.ReceiptID
		resp["receipt_status"] = string(receipt.Status)
	}

	writeJSON(w, http.StatusOK, resp)
}

// GetPacket handles GET /v1/packets/{packet_id}.
func (h *Handler) GetPacket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := pathSuffix(r, "/v1/packets/")
	if id == "" {
		// Fall through to IngestPacket for POST /v1/packets
		h.IngestPacket(w, r)
		return
	}
	p, err := h.store.GetPacket(r.Context(), id)
	if err != nil {
		apiError(w, http.StatusNotFound, "packet not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// ─── AI lane ─────────────────────────────────────────────────────────────────

type aiTraceRequest struct {
	AppID         string `json:"app_id"`
	CorrelationID string `json:"correlation_id"`
	ModelIDHash   string `json:"model_id_hash"`
	PromptHash    string `json:"prompt_hash"`
	ToolCallCount int    `json:"tool_call_count"`
}

// TraceAI handles POST /v1/ai/trace.
func (h *Handler) TraceAI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req aiTraceRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	traceID := newID("ait_")
	if err := h.store.InsertAITrace(r.Context(), traceID, &pgstore.AITraceRecord{
		AppID:         req.AppID,
		CorrelationID: req.CorrelationID,
		ModelIDHash:   req.ModelIDHash,
		PromptHash:    req.PromptHash,
		ToolCallCount: req.ToolCallCount,
	}); err != nil {
		h.log.Error("insert ai trace", zap.Error(err))
		apiError(w, http.StatusInternalServerError, "failed to store AI trace")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"trace_id": traceID, "status": "accepted"})
}

// AuthorizeAI handles POST /v1/ai/authorize.
func (h *Handler) AuthorizeAI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req aiTraceRequest
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	// AI authorize is an authorize packet with category=ai.
	p := &core.Packet{
		SchemaVersion: core.SchemaVersion,
		PacketID:      newID("pkt_"),
		CorrelationID: req.CorrelationID,
		CapturedAt:    time.Now().UTC(),
		App:           core.AppContext{AppID: req.AppID},
		Actor:         core.Actor{Type: core.ActorModel},
		Action: core.Action{
			Category: core.CategoryAI,
			Name:     "ai.model.call",
			Risk:     core.RiskMedium,
			Mutating: false,
		},
		AI: core.AIRecord{
			IsAIRelated:   true,
			ModelIDHash:   req.ModelIDHash,
			PromptHash:    req.PromptHash,
			ToolCallCount: req.ToolCallCount,
		},
	}
	p.Action.Risk = policy.Classify(p)

	decision, err := h.evaluatePolicy(r, p)
	if err != nil {
		decision = &policy.EvaluateResult{Decision: core.DecisionAllow, Reason: "policy degraded"}
	}

	decID := newID("dec_")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"decision":    decision.Decision,
		"decision_id": decID,
		"packet_id":   p.PacketID,
		"reason":      decision.Reason,
	})
}

// RecordAIResult handles POST /v1/ai/result.
func (h *Handler) RecordAIResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body map[string]interface{}
	if err := readJSON(r, &body); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// ─── Ledger ───────────────────────────────────────────────────────────────────

// AnchorPacket handles POST /v1/ledger/anchor.
func (h *Handler) AnchorPacket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		PacketID string `json:"packet_id"`
	}
	if err := readJSON(r, &body); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := h.store.GetPacket(r.Context(), body.PacketID)
	if err != nil {
		apiError(w, http.StatusNotFound, "packet not found")
		return
	}
	receipt := h.enqueueAnchor(r, p)
	writeJSON(w, http.StatusAccepted, receipt)
}

// GetReceipt handles GET /v1/ledger/receipts/{packet_id}.
func (h *Handler) GetReceipt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	packetID := pathSuffix(r, "/v1/ledger/receipts/")
	receipt, err := h.store.GetReceiptByPacketID(r.Context(), packetID)
	if err != nil {
		apiError(w, http.StatusNotFound, "receipt not found")
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

// VerifyReceipt handles GET /v1/ledger/verify/{receipt_id}.
func (h *Handler) VerifyReceipt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	receiptID := pathSuffix(r, "/v1/ledger/verify/")
	_ = receiptID
	// Full verify requires chain backend; return stub result until M4.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"verified":       false,
		"failure_reason": "chain verification pending M4 — receipt stored locally",
	})
}

// ─── Evidence ────────────────────────────────────────────────────────────────

// QueryWindow handles GET /v1/evidence/window.
func (h *Handler) QueryWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	fromStr := q.Get("from")
	toStr := q.Get("to")

	var from, to time.Time
	var err error
	if fromStr != "" {
		from, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			apiError(w, http.StatusBadRequest, "invalid from timestamp")
			return
		}
	}
	if toStr != "" {
		to, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			apiError(w, http.StatusBadRequest, "invalid to timestamp")
			return
		}
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-evidence.DefaultWindowDuration)
	}
	if to.Sub(from) > evidence.DefaultWindowDuration {
		apiError(w, http.StatusBadRequest, "window exceeds 72h operational limit; use export mode")
		return
	}

	appID := q.Get("app_id")
	segments, err := h.store.QuerySegments(r.Context(), appID, from, to)
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"from":     from,
		"to":       to,
		"app_id":   appID,
		"segments": segments,
	})
}

// ExportEvidence handles POST /v1/evidence/export.
func (h *Handler) ExportEvidence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		CorrelationID    string `json:"correlation_id"`
		RedactionProfile string `json:"redaction_profile"`
	}
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.RedactionProfile == "" {
		apiError(w, http.StatusBadRequest, "redaction_profile is required")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"correlation_id":    req.CorrelationID,
		"redaction_profile": req.RedactionProfile,
		"status":            "export_queued",
		"manifest_id":       newID("exp_"),
	})
}

// RewindEvidence handles GET /v1/evidence/rewind/{correlation_id}.
func (h *Handler) RewindEvidence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	corrID := pathSuffix(r, "/v1/evidence/rewind/")
	windowStr := r.URL.Query().Get("window")
	window := evidence.DefaultWindowDuration
	if windowStr != "" {
		if d, err := time.ParseDuration(windowStr); err == nil {
			window = d
		}
	}

	result, err := evidence.Rewind(r.Context(), h.store, corrID, window, false)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ─── Policy ───────────────────────────────────────────────────────────────────

// ListBundles handles GET /v1/policy/bundles.
func (h *Handler) ListBundles(w http.ResponseWriter, r *http.Request) {
	bundles, err := h.store.ListPolicyBundles(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"bundles": bundles})
}

// SimulatePolicy handles POST /v1/policy/simulate.
func (h *Handler) SimulatePolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		ProposedBundleURL string      `json:"proposed_bundle_url"`
		Packet            core.Packet `json:"packet"`
	}
	if err := readJSON(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}

	app, _ := h.store.GetApp(r.Context(), req.Packet.App.AppID)
	result, err := h.policy.Simulate(r.Context(), &policy.EvaluateInput{
		Packet: &req.Packet,
		App:    app,
		Mode:   h.mode,
	}, req.ProposedBundleURL)
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ─── internal helpers ─────────────────────────────────────────────────────────

func (h *Handler) evaluatePolicy(r *http.Request, p *core.Packet) (*policy.EvaluateResult, error) {
	if h.policy == nil {
		return nil, nil
	}
	app, _ := h.store.GetApp(r.Context(), p.App.AppID)
	return h.policy.Evaluate(r.Context(), &policy.EvaluateInput{
		Packet: p,
		App:    app,
		Mode:   h.mode,
	})
}

func (h *Handler) enqueueAnchor(r *http.Request, p *core.Packet) *core.Receipt {
	if h.queue == nil {
		return nil
	}
	pktHash, _ := ledger.HashPacket(p)
	receipt, err := h.queue.Enqueue(r.Context(), &ledger.AnchorRequest{
		Packet:       p,
		PacketHash:   pktHash,
		DecisionHash: "sha256:placeholder",
		BundleHash:   p.Policy.BundleID,
	})
	if err != nil {
		h.log.Warn("anchor enqueue failed", zap.Error(err))
		return nil
	}
	return receipt
}

func isFailClosed(risk core.RiskLevel) bool {
	return risk == core.RiskHigh || risk == core.RiskCritical
}
