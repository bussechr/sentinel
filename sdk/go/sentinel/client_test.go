package sentinel_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/your-org/sentinel/sdk/go/sentinel"
)

// fakeServer returns a test server that always responds with the given decision.
func fakeServer(t *testing.T, decision string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"decision":        decision,
			"decision_id":     "dec_test",
			"packet_id":       "pkt_test",
			"ledger_required": false,
			"receipt_status":  "accepted",
		})
	}))
}

func TestClient_Authorize_Allow(t *testing.T) {
	srv := fakeServer(t, "allow")
	defer srv.Close()

	client := sentinel.NewClient(sentinel.Config{
		Endpoint: srv.URL,
		AppID:    "test-app",
		Mode:     sentinel.ModeGuard,
	})

	resp, err := client.Authorize(context.Background(), "corr_test", sentinel.RoutePolicy{
		ActionName: "test.action",
		Category:   "http",
		Risk:       "low",
	}, "sha256:abc")

	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "allow" {
		t.Errorf("expected allow, got %q", resp.Decision)
	}
}

func TestClient_Authorize_Deny_GuardBlocks(t *testing.T) {
	srv := fakeServer(t, "deny")
	defer srv.Close()

	client := sentinel.NewClient(sentinel.Config{
		Endpoint: srv.URL,
		AppID:    "test-app",
		Mode:     sentinel.ModeGuard,
	})

	handler := client.HTTPMiddleware(sentinel.RoutePolicy{
		ActionName: "test.action",
		Risk:       "high",
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("guard mode should block deny decisions, got %d", w.Code)
	}
}

func TestClient_Authorize_Deny_ObserveAllows(t *testing.T) {
	srv := fakeServer(t, "deny")
	defer srv.Close()

	client := sentinel.NewClient(sentinel.Config{
		Endpoint: srv.URL,
		AppID:    "test-app",
		Mode:     sentinel.ModeObserve,
	})

	handler := client.HTTPMiddleware(sentinel.RoutePolicy{
		ActionName: "test.action",
		Risk:       "high",
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("observe mode should allow even on deny, got %d", w.Code)
	}
}

func TestClient_Authorize_DegradedObserveFailsOpen(t *testing.T) {
	// Point at a non-existent server to simulate Sentinel being down.
	client := sentinel.NewClient(sentinel.Config{
		Endpoint: "http://127.0.0.1:1",
		AppID:    "test-app",
		Mode:     sentinel.ModeObserve,
	})

	resp, err := client.Authorize(context.Background(), "corr_test", sentinel.RoutePolicy{
		ActionName: "test.action",
		Risk:       "low",
	}, "sha256:abc")

	if err != nil {
		t.Fatal("observe mode should fail open, not return error")
	}
	if resp.Decision != "allow" {
		t.Errorf("degraded observe should return allow, got %q", resp.Decision)
	}
}

func TestCorrelationIDFromContext(t *testing.T) {
	srv := fakeServer(t, "allow")
	defer srv.Close()

	client := sentinel.NewClient(sentinel.Config{
		Endpoint: srv.URL,
		AppID:    "test-app",
		Mode:     sentinel.ModeObserve,
	})

	var capturedCorrID string
	handler := client.HTTPMiddleware(sentinel.RoutePolicy{
		ActionName: "test.action",
		Risk:       "low",
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCorrID = sentinel.CorrelationIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Correlation-ID", "corr_injected")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedCorrID != "corr_injected" {
		t.Errorf("expected corr_injected in context, got %q", capturedCorrID)
	}
}
