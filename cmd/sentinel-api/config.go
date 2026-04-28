package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the Sentinel API configuration.
// Sensitive values (DSN, signing keys) are NOT in this struct;
// they are loaded from environment variables or mounted secrets.
type Config struct {
	Sentinel struct {
		Mode             string        `yaml:"mode"`
		Environment      string        `yaml:"environment"`
		EvidenceWindow   time.Duration `yaml:"evidence_window"`
		DefaultRedaction string        `yaml:"default_redaction_profile"`
	} `yaml:"sentinel"`

	API struct {
		Listen       string `yaml:"listen"`
		MTLSRequired bool   `yaml:"mtls_required"`
	} `yaml:"api"`

	Identity struct {
		Provider    string `yaml:"provider"`
		TrustDomain string `yaml:"trust_domain"`
	} `yaml:"identity"`

	Policy struct {
		Engine            string `yaml:"engine"`
		BundleURL         string `yaml:"bundle_url"`
		DecisionLog       bool   `yaml:"decision_log"`
		SimulationEnabled bool   `yaml:"simulation_enabled"`
	} `yaml:"policy"`

	Ledger struct {
		Backend        string   `yaml:"backend"`
		AnchorStrategy string   `yaml:"anchor_strategy"`
		FailClosedFor  []string `yaml:"fail_closed_for"`
	} `yaml:"ledger"`

	Storage struct {
		PostgresDSNSecret string `yaml:"postgres_dsn_secret"`
		ObjectStoreBucket string `yaml:"object_store_bucket"`
		LocalWALPath      string `yaml:"local_wal_path"`
	} `yaml:"storage"`

	Observability struct {
		OTLPEndpoint string `yaml:"otel_endpoint"`
		MetricsPath  string `yaml:"metrics_path"`
	} `yaml:"observability"`
}

// defaultConfig returns sensible defaults for local development.
func defaultConfig() *Config {
	var cfg Config
	cfg.Sentinel.Mode = "observe"
	cfg.Sentinel.Environment = "dev"
	cfg.Sentinel.EvidenceWindow = 72 * time.Hour
	cfg.Sentinel.DefaultRedaction = "default-dev"
	cfg.API.Listen = "0.0.0.0:8080"
	cfg.API.MTLSRequired = false
	cfg.Identity.Provider = "local"
	cfg.Identity.TrustDomain = "sentinel.local"
	cfg.Policy.Engine = "opa"
	cfg.Policy.DecisionLog = true
	cfg.Policy.SimulationEnabled = true
	cfg.Ledger.Backend = "cometbft"
	cfg.Ledger.AnchorStrategy = "risk_tiered"
	cfg.Ledger.FailClosedFor = []string{"high", "critical"}
	cfg.Storage.ObjectStoreBucket = "sentinel-evidence"
	cfg.Storage.LocalWALPath = "/var/lib/sentinel/wal"
	cfg.Observability.OTLPEndpoint = "otel-collector:4317"
	cfg.Observability.MetricsPath = "/metrics"
	return &cfg
}

// loadConfig reads the config file at SENTINEL_CONFIG_FILE, or returns defaults.
func loadConfig() (*Config, error) {
	cfg := defaultConfig()

	path := os.Getenv("SENTINEL_CONFIG_FILE")
	if path == "" {
		return cfg, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config: open %q: %w", path, err)
	}
	defer f.Close()

	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		return nil, fmt.Errorf("config: decode %q: %w", path, err)
	}
	return cfg, nil
}
