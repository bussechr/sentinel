package ai_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	ailane "github.com/your-org/sentinel/internal/capture/ai"
	"github.com/your-org/sentinel/internal/core"
)

func TestGate_Verify_HumanAllowed(t *testing.T) {
	g := ailane.NewGate()
	r := httptest.NewRequest(http.MethodPost, "/v1/packets/authorize", nil)
	p := &core.Packet{
		Actor:  core.Actor{Type: core.ActorHuman},
		Action: core.Action{Category: core.CategoryHTTP},
	}
	if err := g.Verify(r, p); err != nil {
		t.Errorf("human actor should pass: %v", err)
	}
}

func TestGate_Verify_ModelMissingRoute(t *testing.T) {
	g := ailane.NewGate()
	r := httptest.NewRequest(http.MethodPost, "/v1/ai/authorize", nil)
	r.Header.Set(ailane.HeaderActor, "model:claude")
	p := &core.Packet{
		Actor:  core.Actor{Type: core.ActorModel},
		Action: core.Action{Category: core.CategoryAI},
	}
	if err := g.Verify(r, p); !errors.Is(err, ailane.ErrMissingAIRoute) {
		t.Errorf("expected ErrMissingAIRoute, got %v", err)
	}
}

func TestGate_Verify_ModelWithRoute(t *testing.T) {
	g := ailane.NewGate()
	r := httptest.NewRequest(http.MethodPost, "/v1/ai/authorize", nil)
	r.Header.Set(ailane.HeaderActor, "model:claude")
	r.Header.Set(ailane.HeaderRoute, ailane.HeaderRouteValue)
	p := &core.Packet{
		Actor:  core.Actor{Type: core.ActorModel},
		Action: core.Action{Category: core.CategoryAI},
	}
	if err := g.Verify(r, p); err != nil {
		t.Errorf("model with route should pass: %v", err)
	}
}

func TestGate_Verify_AICategoryMissingActor(t *testing.T) {
	g := ailane.NewGate()
	r := httptest.NewRequest(http.MethodPost, "/v1/ai/authorize", nil)
	p := &core.Packet{
		Actor:  core.Actor{Type: core.ActorService},
		Action: core.Action{Category: core.CategoryAI},
	}
	if err := g.Verify(r, p); !errors.Is(err, ailane.ErrMissingAIActor) {
		t.Errorf("expected ErrMissingAIActor, got %v", err)
	}
}

func TestGate_EnforceMiddleware(t *testing.T) {
	g := ailane.NewGate()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := g.EnforceMiddleware(next)

	t.Run("missing-route", func(t *testing.T) {
		called = false
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/v1/ai/authorize", nil)
		mw.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
		if called {
			t.Error("downstream handler should not have run")
		}
	})

	t.Run("present", func(t *testing.T) {
		called = false
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/v1/ai/authorize", nil)
		r.Header.Set(ailane.HeaderRoute, ailane.HeaderRouteValue)
		r.Header.Set(ailane.HeaderActor, "agent:test")
		mw.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if !called {
			t.Error("downstream handler should have run")
		}
	})
}
