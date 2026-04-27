// Example: go-http-app demonstrates minimal Go SDK integration.
//
// This app registers two routes:
//   /refund    — high-risk mutating action (guard mode)
//   /status    — low-risk read-only action (observe mode)
//
// Run:
//   SENTINEL_API_ENDPOINT=http://localhost:8080 go run ./examples/go-http-app/
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/your-org/sentinel/sdk/go/sentinel"
)

func main() {
	endpoint := os.Getenv("SENTINEL_API_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8080"
	}

	client := sentinel.NewClient(sentinel.Config{
		Endpoint: endpoint,
		AppID:    "billing-api",
		Mode:     sentinel.ModeGuard,
	})

	mux := http.NewServeMux()

	// High-risk mutating route — guard mode will block on deny.
	mux.Handle("/refund", client.HTTPMiddleware(
		sentinel.RoutePolicy{
			ActionName:   "invoice.refund.create",
			Category:     "http",
			Risk:         "high",
			Mutating:     true,
			ResourceType: "invoice",
		},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintln(w, `{"status":"refund accepted"}`)
		}),
	))

	// Low-risk read route — advisory only.
	mux.Handle("/status", client.HTTPMiddleware(
		sentinel.RoutePolicy{
			ActionName: "billing.status.read",
			Category:   "http",
			Risk:       "low",
			Mutating:   false,
		},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			corrID := sentinel.CorrelationIDFromContext(r.Context())
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"status":"ok","correlation_id":%q}`, corrID)
		}),
	))

	addr := ":9090"
	fmt.Printf("billing-api example listening on %s (sentinel at %s)\n", addr, endpoint)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
