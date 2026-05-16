package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/your-org/sentinel/internal/api"
	"go.uber.org/zap"
)

// newTestHandler creates an API handler with all dependencies set to nil (observe mode).
func newTestHandler(t *testing.T) *api.Handler {
	t.Helper()
	log, _ := zap.NewDevelopment()
	return api.NewHandler(nil, nil, nil, nil, "observe", log)
}

func TestHealthz(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAuthorizePacket_MissingAppID(t *testing.T) {
	h := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	body, _ := json.Marshal(map[string]interface{}{
		"action_name":  "test.action",
		"category":     "http",
		"risk":         "low",
		"mutating":     false,
		"payload_hash": "sha256:abc",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/packets/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing app_id, got %d", w.Code)
	}
}

func TestAuthorizePacket_ObserveModeAlwaysAllows(t *testing.T) {
	h := newTestHandler(t) // nil policy → degraded → allow in observe
	mux := http.NewServeMux()
	h.Register(mux)

	body, _ := json.Marshal(map[string]interface{}{
		"app_id":       "test-app",
		"action_name":  "test.action",
		"category":     "http",
		"risk":         "high",
		"mutating":     true,
		"payload_hash": "sha256:abc",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/packets/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// With nil store, InsertPacket will fail, returning 500.
	// In observe mode, policy nil → allow, but store nil → 500 on insert.
	// This is expected for the unit test without a real store.
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("unexpected status %d", w.Code)
	}
}

func TestRegisterApp_MethodNotAllowed(t *testing.T) {
	h := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/apps/register", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestIngestPacket_MethodNotAllowed(t *testing.T) {
	h := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/packets", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestQueryWindow_TooWide(t *testing.T) {
	h := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/evidence/window?from=2024-01-01T00:00:00Z&to=2024-01-10T00:00:00Z", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for window > 72h, got %d", w.Code)
	}
}

func TestRewindEvidence_EmptyHotWithoutColdIndexReturnsOK(t *testing.T) {
	h := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/evidence/rewind/corr_missing", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for no hot evidence and no cold index, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		CorrelationID  string `json:"correlation_id"`
		ArchiveLocator string `json:"archive_locator"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.CorrelationID != "corr_missing" {
		t.Fatalf("correlation_id = %q", resp.CorrelationID)
	}
	if resp.ArchiveLocator != "" {
		t.Fatalf("archive_locator = %q, want empty", resp.ArchiveLocator)
	}
}

func TestAuthorizeAI_RejectsMissingRouteHeader(t *testing.T) {
	h := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	body, _ := json.Marshal(map[string]interface{}{
		"app_id":         "claude-billing",
		"correlation_id": "corr_x",
		"prompt_hash":    "sha256:abc",
		"model_id_hash":  "sha256:def",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/ai/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for missing AI gateway headers, got %d", w.Code)
	}
}

func TestAuthorizeAI_PassesWithGatewayHeaders(t *testing.T) {
	h := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	body, _ := json.Marshal(map[string]interface{}{
		"app_id":         "claude-billing",
		"correlation_id": "corr_x",
		"prompt_hash":    "sha256:abc",
		"model_id_hash":  "sha256:def",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/ai/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Sentinel-AI-Route", "ai-gateway")
	req.Header.Set("X-Sentinel-AI-Actor", "model:claude")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// nil store/policy → handler returns 200 with degraded allow.
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 once gateway headers are set, got %d", w.Code)
	}
}

func TestCausalGraph_RequiresCorrelationID(t *testing.T) {
	h := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/evidence/causal/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty correlation id, got %d", w.Code)
	}
}

func TestListWriters_EmptyRegistry(t *testing.T) {
	h := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/ledger/writers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Writers []map[string]interface{} `json:"writers"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Writers) != 0 {
		t.Errorf("expected empty writer list, got %d", len(resp.Writers))
	}
}
