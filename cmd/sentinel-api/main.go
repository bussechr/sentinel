// sentinel-api is the Sentinel control plane.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/your-org/sentinel/internal/api"
	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/ledger"
	"github.com/your-org/sentinel/internal/observability"
	"github.com/your-org/sentinel/internal/policy"
	"github.com/your-org/sentinel/internal/store/postgres"
)

func main() {
	log, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sentinel-api: failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync() //nolint:errcheck

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── Config ───────────────────────────────────────────────────────────────
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal("load config", zap.Error(err))
	}

	// ── Postgres ─────────────────────────────────────────────────────────────
	dsn := os.Getenv("SENTINEL_POSTGRES_DSN")
	if dsn == "" {
		// Fall back to secret file (Docker/Kubernetes mount).
		if b, err := os.ReadFile("/run/secrets/sentinel_postgres_dsn"); err == nil {
			dsn = string(b)
		}
	}
	if dsn == "" {
		log.Fatal("SENTINEL_POSTGRES_DSN env var or /run/secrets/sentinel_postgres_dsn secret required")
	}

	store, err := postgres.New(ctx, dsn)
	if err != nil {
		log.Fatal("postgres init", zap.Error(err))
	}
	defer store.Close()

	// ── OTel ─────────────────────────────────────────────────────────────────
	if cfg.Observability.OTLPEndpoint != "" {
		otelShutdown, err := observability.InitTracer(ctx, observability.Config{
			OTLPEndpoint: cfg.Observability.OTLPEndpoint,
			ServiceName:  "sentinel-api",
			Version:      "v1",
		}, log)
		if err != nil {
			log.Warn("otel init failed (continuing without tracing)", zap.Error(err))
		} else {
			defer otelShutdown(context.Background()) //nolint:errcheck
		}
	}

	// ── Policy engine (OPA) ──────────────────────────────────────────────────
	var policyEngine *policy.Engine
	if cfg.Policy.BundleURL != "" {
		policyEngine, err = policy.NewEngine(ctx, cfg.Policy.BundleURL, log)
		if err != nil {
			log.Warn("policy engine init failed — running in degraded mode", zap.Error(err))
		}
	}

	// ── Ledger ───────────────────────────────────────────────────────────────
	// For now, use a nil backend (anchor queue logs but doesn't chain-submit).
	// M4 replaces this with the CometBFT backend.
	var anchorQueue *ledger.Queue
	if policyEngine != nil {
		// Only enable the anchor queue when policy is also available.
		anchorQueue = ledger.NewQueue(nil, log)
	}

	witness, err := ledger.NewWitness()
	if err != nil {
		log.Fatal("witness init", zap.Error(err))
	}

	// ── HTTP mux ─────────────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Health & readiness probes.
	readinessChecks := []observability.ReadinessCheck{
		{Name: "postgres", Check: store.Ping},
	}
	observability.RegisterHTTPHandlers(mux, readinessChecks)

	// Prometheus metrics stub.
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("# Sentinel metrics — OTel collector is the primary sink\n"))
	})

	// Register all v1 API handlers.
	mode := core.SentinelMode(cfg.Sentinel.Mode)
	if mode == "" {
		mode = core.ModeObserve
	}
	apiHandler := api.NewHandler(store, policyEngine, anchorQueue, witness, mode, log)
	if policyEngine != nil && cfg.Policy.ShadowBundleURL != "" {
		apiHandler.WithShadow(policy.NewShadow(
			policyEngine,
			cfg.Policy.ShadowBundleURL,
			cfg.Policy.ShadowBundleID,
			log,
		))
	}
	apiHandler.Register(mux)

	srv := &http.Server{
		Addr:         cfg.API.Listen,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info("sentinel-api listening",
			zap.String("addr", cfg.API.Listen),
			zap.String("mode", string(mode)),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("http serve", zap.Error(err))
		}
	}()

	<-ctx.Done()
	log.Info("sentinel-api shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Error("http shutdown", zap.Error(err))
	}
}
