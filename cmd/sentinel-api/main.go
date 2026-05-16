// sentinel-api is the Sentinel control plane.
package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/your-org/sentinel/internal/api"
	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/evidence"
	"github.com/your-org/sentinel/internal/ledger"
	"github.com/your-org/sentinel/internal/observability"
	"github.com/your-org/sentinel/internal/policy"
	"github.com/your-org/sentinel/internal/store/object"
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

	objectStore, err := buildObjectStore(cfg, log)
	if err != nil {
		log.Warn("object store init failed; cold archive disabled", zap.Error(err))
	}

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
	var anchorQueue *ledger.Queue
	writerRegistry := ledger.NewRegistry()
	if policyEngine != nil {
		// Only enable the anchor queue when policy is also available.
		backend := buildLedgerBackend(cfg, log)
		if backend != nil {
			if w, ok := backend.(ledger.Writer); ok {
				writerRegistry.Register(w)
			} else {
				writerRegistry.Register(ledger.AsWriter(ledger.WriterCometBFT, "cometbft-default", backend))
			}
		}
		anchorQueue = ledger.NewQueue(backend, log).WithDurableStore(store)
		leader := postgres.NewAdvisoryLockLeaderElector(store, podIdentity(), log)
		leader.Start(ctx)
		defer leader.Close()
		anchorQueue.WithLeaderElector(leader)
		go drainAnchorQueue(ctx, anchorQueue)
	}
	if objectStore != nil && cfg.Storage.ObjectStoreBucket != "" {
		go runArchiver(ctx, store, objectStore, cfg.Storage.ObjectStoreBucket, log)
	}

	witness, err := ledger.NewWitness()
	if err != nil {
		log.Fatal("witness init", zap.Error(err))
	}

	// ── HTTP mux ─────────────────────────────────────────────────────────────
	mux := http.NewServeMux()
	mode := core.SentinelMode(cfg.Sentinel.Mode)
	if mode == "" {
		mode = core.ModeObserve
	}
	apiHandler := api.NewHandler(store, policyEngine, anchorQueue, witness, mode, log)
	apiHandler.WithWriterRegistry(writerRegistry)
	apiHandler.WithObjectStore(objectStore)
	if policyEngine != nil && cfg.Policy.ShadowBundleURL != "" {
		apiHandler.WithShadow(policy.NewShadow(
			policyEngine,
			cfg.Policy.ShadowBundleURL,
			cfg.Policy.ShadowBundleID,
			log,
		))
	}

	// Health & readiness probes.
	readinessChecks := []observability.ReadinessCheck{
		{Name: "postgres", Check: store.Ping},
	}
	observability.RegisterHTTPHandlers(mux, readinessChecks)

	// Prometheus metrics.
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("# Sentinel metrics - OTel collector is the primary sink\n"))
		if anchorQueue != nil {
			pod, isLeader := anchorQueue.LeaderMetric(r.Context())
			depth := anchorQueue.PendingDepth(r.Context())
			fmt.Fprintf(w, "sentinel_anchor_queue_leader{pod=%q} %d\n", pod, isLeader)
			fmt.Fprintf(w, "sentinel_anchor_queue_depth %d\n", depth)
		}
		for _, h := range writerRegistry.HealthAll(r.Context()) {
			healthy := 0
			if h.Healthy {
				healthy = 1
			}
			fmt.Fprintf(w, "sentinel_writer_healthy{kind=%q,name=%q} %d\n", h.Kind, h.Name, healthy)
			fmt.Fprintf(w, "sentinel_writer_height{kind=%q,name=%q} %d\n", h.Kind, h.Name, h.Height)
		}
		coldHits, coldLatency := apiHandler.ColdArchiveMetrics()
		fmt.Fprintf(w, "sentinel_evidence_cold_hits_total %d\n", coldHits)
		fmt.Fprintf(w, "sentinel_evidence_cold_lookup_latency_seconds %f\n", coldLatency)
	})

	// Register all v1 API handlers.
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

func buildObjectStore(cfg *Config, log *zap.Logger) (object.Store, error) {
	endpoint := strings.TrimSpace(os.Getenv("SENTINEL_OBJECT_STORE_ENDPOINT"))
	if endpoint == "" {
		log.Info("object store endpoint not configured; cold archive reads disabled")
		return nil, nil
	}
	useSSL := strings.EqualFold(os.Getenv("SENTINEL_OBJECT_STORE_SSL"), "true")
	insecureTLS := strings.EqualFold(os.Getenv("SENTINEL_OBJECT_STORE_INSECURE_TLS"), "true")
	store, err := object.NewMinio(object.MinioConfig{
		Endpoint:        endpoint,
		AccessKeyID:     os.Getenv("SENTINEL_OBJECT_STORE_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("SENTINEL_OBJECT_STORE_SECRET_ACCESS_KEY"),
		UseSSL:          useSSL,
		Region:          os.Getenv("SENTINEL_OBJECT_STORE_REGION"),
		InsecureTLS:     insecureTLS,
	})
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(os.Getenv("SENTINEL_OBJECT_STORE_CREATE_BUCKET"), "true") {
		if err := store.EnsureBucket(context.Background(), cfg.Storage.ObjectStoreBucket, os.Getenv("SENTINEL_OBJECT_STORE_REGION")); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func buildLedgerBackend(cfg *Config, log *zap.Logger) ledger.Backend {
	if strings.ToLower(cfg.Ledger.Backend) != "cometbft" {
		return nil
	}
	rpc := strings.TrimSpace(os.Getenv("SENTINEL_COMETBFT_RPC"))
	if rpc == "" {
		rpc = "http://sentinel-cometbft:26657"
	}
	key, err := loadChainKey()
	if err != nil {
		log.Warn("chain key unavailable; CometBFT receipts will be unsigned", zap.Error(err))
	}
	return ledger.NewCometBFTBackend(rpc, key, log)
}

func loadChainKey() ([]byte, error) {
	path := strings.TrimSpace(os.Getenv("SENTINEL_CHAIN_KEY_FILE"))
	if path == "" {
		path = "/run/secrets/sentinel_chain_key"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := strings.TrimSpace(string(raw))
	if decoded, err := hex.DecodeString(strings.TrimPrefix(s, "ed25519:")); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
		return decoded, nil
	}
	return raw, nil
}

func podIdentity() string {
	for _, key := range []string{"POD_NAME", "HOSTNAME"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return "sentinel-api"
}

func drainAnchorQueue(ctx context.Context, queue *ledger.Queue) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			queue.DrainBatch(ctx)
		}
	}
}

func runArchiver(ctx context.Context, store *postgres.Store, objectStore object.Store, bucket string, log *zap.Logger) {
	archiver := evidence.NewArchiver(
		store,
		evidence.NewObjectStoreSink(objectStore, bucket),
		store,
		evidence.DefaultWindowDuration,
		log,
	)
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		if _, err := archiver.Run(ctx, 50); err != nil && ctx.Err() == nil {
			log.Warn("cold archive run failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
