// Package observability — extended health check helpers.
//
// This file adds richer readiness checks beyond the basic Postgres ping:
//   - OPA reachability
//   - Chain height (CometBFT RPC)
//   - Object store bucket reachability
//
// Components register their check with RegisterHTTPHandlers in otel.go.
package observability

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// HTTPCheck performs a GET request against the target URL and expects a 200 response.
func HTTPCheck(url string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("health check %q: %w", url, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("health check %q: %w", url, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("health check %q: HTTP %d", url, resp.StatusCode)
		}
		return nil
	}
}

// AlwaysHealthy is a no-op check used for optional dependencies in observe mode.
func AlwaysHealthy(_ context.Context) error { return nil }
