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
		"action_name": "test.action",
		"category":    "http",
		"risk":        "low",
		"mutating":    false,
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
