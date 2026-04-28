// Package commands contains all sentinelctl subcommand implementations.
//
// Each subcommand is a thin HTTP client over the Sentinel API. The endpoint
// is resolved from the --endpoint flag, then SENTINEL_API_ENDPOINT, then
// the local default http://localhost:8080. The bearer token comes from
// --token or SENTINEL_API_TOKEN.
//
// Output is human-readable by default. Pass --json on the root command to
// receive raw JSON suitable for piping to jq.
package commands

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// ─── flag/env resolvers ──────────────────────────────────────────────────────

// apiEndpoint resolves the Sentinel API endpoint from flag or environment.
func apiEndpoint(cmd *cobra.Command) string {
	if ep, _ := cmd.Flags().GetString("endpoint"); ep != "" {
		return ep
	}
	if ep := os.Getenv("SENTINEL_API_ENDPOINT"); ep != "" {
		return ep
	}
	return "http://localhost:8080"
}

// apiToken resolves the bearer token from flag or environment.
func apiToken(cmd *cobra.Command) string {
	if t, _ := cmd.Flags().GetString("token"); t != "" {
		return t
	}
	return os.Getenv("SENTINEL_API_TOKEN")
}

// jsonMode reports whether --json was set on the root command.
func jsonMode(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("json")
	return v
}

// printJSON pretty-prints v as JSON to stdout.
func printJSON(v interface{}) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "json marshal error: %v\n", err)
		return
	}
	fmt.Println(string(b))
}

// ─── HTTP transport ──────────────────────────────────────────────────────────

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

func doRequest(cmd *cobra.Command, method, path string, body interface{}, extraHeaders map[string]string) ([]byte, int, error) {
	endpoint := apiEndpoint(cmd)
	full, err := url.JoinPath(endpoint, path)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid path %q: %w", path, err)
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, full, reader)
	if err != nil {
		return nil, 0, err
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t := apiToken(cmd); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := newHTTPClient().Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP %s %s: %w", method, full, err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return respBytes, resp.StatusCode, nil
}

// printResponse renders an API response either as raw JSON (--json) or
// as a friendlier human summary using the supplied label.
func printResponse(cmd *cobra.Command, label string, raw []byte, status int) error {
	if status >= 400 {
		fmt.Fprintf(os.Stderr, "✗ %s failed (HTTP %d)\n", label, status)
		fmt.Fprintln(os.Stderr, string(raw))
		return fmt.Errorf("%s failed: HTTP %d", label, status)
	}
	if jsonMode(cmd) {
		os.Stdout.Write(raw)
		fmt.Println()
		return nil
	}
	if len(raw) == 0 {
		fmt.Printf("✓ %s ok\n", label)
		return nil
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		fmt.Println(string(raw))
		return nil
	}
	fmt.Printf("✓ %s\n%s\n", label, pretty.String())
	return nil
}

// ─── doctor ──────────────────────────────────────────────────────────────────

// DoctorCmd checks connectivity and dependency health.
func DoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check Sentinel connectivity and dependency health",
		RunE: func(cmd *cobra.Command, args []string) error {
			ep := apiEndpoint(cmd)
			fmt.Printf("Probing sentinel-api at %s\n", ep)

			ok := true
			for _, probe := range []struct{ label, path string }{
				{"liveness  /healthz", "/healthz"},
				{"readiness /readyz", "/readyz"},
				{"writers   /v1/ledger/writers", "/v1/ledger/writers"},
				{"policy    /v1/policy/bundles", "/v1/policy/bundles"},
			} {
				body, status, err := doRequest(cmd, http.MethodGet, probe.path, nil, nil)
				switch {
				case err != nil:
					fmt.Printf("  ✗ %s — %v\n", probe.label, err)
					ok = false
				case status >= 400:
					fmt.Printf("  ✗ %s — HTTP %d (%s)\n", probe.label, status, summarise(body))
					ok = false
				default:
					fmt.Printf("  ✓ %s — HTTP %d\n", probe.label, status)
				}
			}
			if !ok {
				return fmt.Errorf("doctor: one or more probes failed")
			}
			return nil
		},
	}
}

func summarise(b []byte) string {
	if len(b) > 80 {
		return string(b[:80]) + "..."
	}
	return string(b)
}

// ─── register ────────────────────────────────────────────────────────────────

// RegisterCmd registers apps with Sentinel.
func RegisterCmd() *cobra.Command {
	register := &cobra.Command{
		Use:   "register",
		Short: "Register resources with Sentinel",
	}

	appCmd := &cobra.Command{
		Use:   "app",
		Short: "Register an application",
		Example: `  sentinelctl register app \
    --app-id billing-api \
    --service billing \
    --env prod \
    --owner platform \
    --mode guard`,
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, _ := cmd.Flags().GetString("app-id")
			service, _ := cmd.Flags().GetString("service")
			env, _ := cmd.Flags().GetString("env")
			owner, _ := cmd.Flags().GetString("owner")
			mode, _ := cmd.Flags().GetString("mode")
			riskTier, _ := cmd.Flags().GetString("risk-tier")

			if appID == "" {
				return fmt.Errorf("--app-id is required")
			}
			if service == "" {
				service = appID
			}

			payload := map[string]interface{}{
				"app_id":      appID,
				"service":     service,
				"environment": env,
				"owner":       owner,
				"mode":        mode,
				"risk_tier":   riskTier,
			}
			body, status, err := doRequest(cmd, http.MethodPost, "/v1/apps/register", payload, nil)
			if err != nil {
				return err
			}
			return printResponse(cmd, "app registered", body, status)
		},
	}
	appCmd.Flags().String("app-id", "", "Application ID (required)")
	appCmd.Flags().String("service", "", "Service name (defaults to app-id)")
	appCmd.Flags().String("env", "prod", "Environment")
	appCmd.Flags().String("owner", "", "Owner team")
	appCmd.Flags().String("mode", "observe", "Sentinel mode: observe|guard|enforce")
	appCmd.Flags().String("risk-tier", "low", "Default risk tier: low|medium|high|critical")

	register.AddCommand(appCmd)
	return register
}

// ─── emit-test-packet ────────────────────────────────────────────────────────

// EmitTestPacketCmd emits a test governance packet via /v1/packets/authorize.
func EmitTestPacketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "emit-test-packet",
		Short: "Emit a test governance packet",
		Example: `  sentinelctl emit-test-packet \
    --app-id billing-api \
    --action invoice.refund.create \
    --risk high \
    --mutating`,
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, _ := cmd.Flags().GetString("app-id")
			action, _ := cmd.Flags().GetString("action")
			risk, _ := cmd.Flags().GetString("risk")
			category, _ := cmd.Flags().GetString("category")
			mutating, _ := cmd.Flags().GetBool("mutating")
			corrID, _ := cmd.Flags().GetString("correlation-id")

			if appID == "" {
				return fmt.Errorf("--app-id is required")
			}
			if corrID == "" {
				corrID = "corr_" + uuid.NewString()
			}
			payloadHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(time.Now().Format(time.RFC3339Nano))))

			req := map[string]interface{}{
				"app_id":         appID,
				"actor_type":     "service",
				"action_name":    action,
				"category":       category,
				"risk":           risk,
				"mutating":       mutating,
				"payload_hash":   payloadHash,
				"correlation_id": corrID,
			}
			body, status, err := doRequest(cmd, http.MethodPost, "/v1/packets/authorize", req, nil)
			if err != nil {
				return err
			}
			return printResponse(cmd, fmt.Sprintf("authorize (correlation %s)", corrID), body, status)
		},
	}
	cmd.Flags().String("app-id", "", "Application ID")
	cmd.Flags().String("action", "test.action", "Action name")
	cmd.Flags().String("risk", "low", "Risk level: low|medium|high|critical")
	cmd.Flags().String("category", "http", "Action category: http|grpc|ai|tool|db|file|network|k8s|secret|config")
	cmd.Flags().Bool("mutating", false, "Whether the action mutates state")
	cmd.Flags().String("correlation-id", "", "Override correlation ID (generated when empty)")
	return cmd
}

// ─── simulate-policy ─────────────────────────────────────────────────────────

// SimulatePolicyCmd previews policy decisions before promotion.
func SimulatePolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "simulate-policy",
		Short: "Preview policy decisions before promoting a bundle",
		Example: `  sentinelctl simulate-policy \
    --proposed-bundle https://policy/bundle-v3.tar.gz \
    --packet ./examples/refund.packet.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			bundle, _ := cmd.Flags().GetString("proposed-bundle")
			packetPath, _ := cmd.Flags().GetString("packet")
			if bundle == "" || packetPath == "" {
				return fmt.Errorf("--proposed-bundle and --packet are required")
			}
			raw, err := os.ReadFile(packetPath)
			if err != nil {
				return fmt.Errorf("read packet: %w", err)
			}
			var packet map[string]interface{}
			if err := json.Unmarshal(raw, &packet); err != nil {
				return fmt.Errorf("packet must be JSON: %w", err)
			}
			req := map[string]interface{}{
				"proposed_bundle_url": bundle,
				"packet":              packet,
			}
			body, status, err := doRequest(cmd, http.MethodPost, "/v1/policy/simulate", req, nil)
			if err != nil {
				return err
			}
			return printResponse(cmd, "simulation", body, status)
		},
	}
	cmd.Flags().String("proposed-bundle", "", "URL of the proposed policy bundle")
	cmd.Flags().String("packet", "", "Path to a packet JSON file")
	return cmd
}

// ─── verify-ledger ───────────────────────────────────────────────────────────

// VerifyLedgerCmd verifies a packet receipt against the governance chain.
func VerifyLedgerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "verify-ledger",
		Short:   "Verify a packet receipt against the governance chain",
		Example: `  sentinelctl verify-ledger --packet-id pkt_01HT...`,
		RunE: func(cmd *cobra.Command, args []string) error {
			packetID, _ := cmd.Flags().GetString("packet-id")
			if packetID == "" {
				return fmt.Errorf("--packet-id is required")
			}
			body, status, err := doRequest(cmd, http.MethodGet, "/v1/ledger/receipts/"+url.PathEscape(packetID), nil, nil)
			if err != nil {
				return err
			}
			if status >= 400 {
				return printResponse(cmd, "fetch receipt", body, status)
			}
			var receipt struct {
				ReceiptID string `json:"receipt_id"`
			}
			_ = json.Unmarshal(body, &receipt)
			if jsonMode(cmd) {
				os.Stdout.Write(body)
				fmt.Println()
			} else {
				fmt.Printf("Receipt: %s\n", receipt.ReceiptID)
			}
			if receipt.ReceiptID == "" {
				return nil
			}
			vbody, vstatus, err := doRequest(cmd, http.MethodGet, "/v1/ledger/verify/"+url.PathEscape(receipt.ReceiptID), nil, nil)
			if err != nil {
				return err
			}
			return printResponse(cmd, "ledger verify", vbody, vstatus)
		},
	}
	cmd.Flags().String("packet-id", "", "Packet ID to verify")
	return cmd
}

// ─── rewind ──────────────────────────────────────────────────────────────────

// RewindCmd reconstructs the evidence path for a correlation ID.
func RewindCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rewind",
		Short: "Reconstruct evidence for a correlation ID",
		Example: `  sentinelctl rewind --correlation-id corr_01HT... --window 72h
  sentinelctl rewind --correlation-id corr_01HT... --graph`,
		RunE: func(cmd *cobra.Command, args []string) error {
			corrID, _ := cmd.Flags().GetString("correlation-id")
			window, _ := cmd.Flags().GetString("window")
			graph, _ := cmd.Flags().GetBool("graph")
			if corrID == "" {
				return fmt.Errorf("--correlation-id is required")
			}
			path := "/v1/evidence/rewind/" + url.PathEscape(corrID)
			label := "rewind"
			if graph {
				path = "/v1/evidence/causal/" + url.PathEscape(corrID)
				label = "causal graph"
			}
			if window != "" {
				path += "?window=" + url.QueryEscape(window)
			}
			body, status, err := doRequest(cmd, http.MethodGet, path, nil, nil)
			if err != nil {
				return err
			}
			return printResponse(cmd, label, body, status)
		},
	}
	cmd.Flags().String("correlation-id", "", "Correlation ID to rewind")
	cmd.Flags().String("window", "72h", "Evidence window (max 72h for operational mode)")
	cmd.Flags().Bool("graph", false, "Return the compiled causal graph instead of raw rewind")
	return cmd
}

// ─── export-evidence ─────────────────────────────────────────────────────────

// ExportEvidenceCmd produces a signed evidence export bundle.
func ExportEvidenceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-evidence",
		Short: "Produce a signed evidence export bundle",
		Example: `  sentinelctl export-evidence \
    --correlation-id corr_01HT... \
    --redaction-profile default-prod`,
		RunE: func(cmd *cobra.Command, args []string) error {
			corrID, _ := cmd.Flags().GetString("correlation-id")
			profile, _ := cmd.Flags().GetString("redaction-profile")
			if corrID == "" || profile == "" {
				return fmt.Errorf("--correlation-id and --redaction-profile are required")
			}
			req := map[string]interface{}{
				"correlation_id":    corrID,
				"redaction_profile": profile,
			}
			body, status, err := doRequest(cmd, http.MethodPost, "/v1/evidence/export", req, nil)
			if err != nil {
				return err
			}
			return printResponse(cmd, "export queued", body, status)
		},
	}
	cmd.Flags().String("correlation-id", "", "Correlation ID to export")
	cmd.Flags().String("redaction-profile", "", "Redaction profile (required)")
	return cmd
}

// ─── writers (multi-backend ledger health) ───────────────────────────────────

// WritersCmd reports the health of every registered ledger writer.
func WritersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "writers",
		Short: "Show health of every registered ledger writer (CometBFT, Besu, immudb)",
		RunE: func(cmd *cobra.Command, args []string) error {
			body, status, err := doRequest(cmd, http.MethodGet, "/v1/ledger/writers", nil, nil)
			if err != nil {
				return err
			}
			return printResponse(cmd, "writer health", body, status)
		},
	}
}

// ─── shadow-divergences ──────────────────────────────────────────────────────

// ShadowDivergencesCmd lists recent shadow vs active policy divergences.
func ShadowDivergencesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "shadow-divergences",
		Short:   "List recent shadow vs active policy divergences",
		Example: `  sentinelctl shadow-divergences --since 2h --limit 20`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sinceStr, _ := cmd.Flags().GetString("since")
			limit, _ := cmd.Flags().GetInt("limit")

			path := "/v1/policy/shadow/divergences"
			q := url.Values{}
			q.Set("limit", strconv.Itoa(limit))
			if sinceStr != "" {
				if d, err := time.ParseDuration(sinceStr); err == nil {
					q.Set("since", time.Now().Add(-d).UTC().Format(time.RFC3339))
				} else if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
					q.Set("since", t.UTC().Format(time.RFC3339))
				} else {
					return fmt.Errorf("--since must be a duration (e.g. 2h) or RFC3339 timestamp")
				}
			}
			path += "?" + q.Encode()
			body, status, err := doRequest(cmd, http.MethodGet, path, nil, nil)
			if err != nil {
				return err
			}
			return printResponse(cmd, "shadow divergences", body, status)
		},
	}
	cmd.Flags().String("since", "24h", "Lookback window (Go duration, e.g. 2h, 30m) or RFC3339 timestamp")
	cmd.Flags().Int("limit", 50, "Max rows to return")
	return cmd
}
