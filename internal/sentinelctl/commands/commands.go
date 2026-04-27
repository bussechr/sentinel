// Package commands contains all sentinelctl subcommand implementations.
package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

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

// printJSON pretty-prints v as JSON to stdout.
func printJSON(v interface{}) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "json marshal error: %v\n", err)
		return
	}
	fmt.Println(string(b))
}

// ─── doctor ──────────────────────────────────────────────────────────────────

// DoctorCmd checks connectivity and dependency health.
func DoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check Sentinel connectivity and dependency health",
		RunE: func(cmd *cobra.Command, args []string) error {
			ep := apiEndpoint(cmd)
			fmt.Printf("Checking sentinel-api at %s ...\n", ep)

			// TODO: HTTP GET /healthz and /readyz, print results.
			// TODO: verify Postgres reachable via /readyz.
			// TODO: verify OPA reachable.
			// TODO: verify chain reachable via /v1/ledger/anchor stub.

			fmt.Println("✓ sentinel-api /healthz OK (stub)")
			fmt.Println("✓ sentinel-api /readyz OK (stub)")
			return nil
		},
	}
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
    --env prod \
    --owner platform \
    --mode guard`,
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, _ := cmd.Flags().GetString("app-id")
			env, _ := cmd.Flags().GetString("env")
			owner, _ := cmd.Flags().GetString("owner")
			mode, _ := cmd.Flags().GetString("mode")

			if appID == "" {
				return fmt.Errorf("--app-id is required")
			}

			payload := map[string]string{
				"app_id":      appID,
				"environment": env,
				"owner":       owner,
				"mode":        mode,
			}

			// TODO: POST to /v1/apps/register and print the response.
			fmt.Printf("Registering app %q in %s mode (endpoint: %s)...\n", appID, mode, apiEndpoint(cmd))
			printJSON(payload)
			fmt.Println("✓ Registered (stub — M1)")
			return nil
		},
	}
	appCmd.Flags().String("app-id", "", "Application ID (required)")
	appCmd.Flags().String("env", "prod", "Environment")
	appCmd.Flags().String("owner", "", "Owner team")
	appCmd.Flags().String("mode", "observe", "Sentinel mode: observe|guard|enforce")

	register.AddCommand(appCmd)
	return register
}

// ─── emit-test-packet ────────────────────────────────────────────────────────

// EmitTestPacketCmd emits a test governance packet.
func EmitTestPacketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "emit-test-packet",
		Short: "Emit a test governance packet",
		Example: `  sentinelctl emit-test-packet \
    --app-id billing-api \
    --action invoice.refund.create \
    --risk high \
    --mutating true`,
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, _ := cmd.Flags().GetString("app-id")
			action, _ := cmd.Flags().GetString("action")
			risk, _ := cmd.Flags().GetString("risk")
			mutating, _ := cmd.Flags().GetBool("mutating")

			packet := map[string]interface{}{
				"schema_version": "sentinel.packet.v1",
				"app_id":         appID,
				"action_name":    action,
				"risk":           risk,
				"mutating":       mutating,
				"captured_at":    time.Now().UTC(),
				"test":           true,
			}

			// TODO: POST to /v1/packets and print receipt.
			fmt.Printf("Emitting test packet for app %q action %q risk=%s ...\n", appID, action, risk)
			printJSON(packet)
			fmt.Println("✓ Test packet accepted (stub — M1)")
			return nil
		},
	}
	cmd.Flags().String("app-id", "", "Application ID")
	cmd.Flags().String("action", "test.action", "Action name")
	cmd.Flags().String("risk", "low", "Risk level: low|medium|high|critical")
	cmd.Flags().Bool("mutating", false, "Whether the action mutates state")
	return cmd
}

// ─── simulate-policy ─────────────────────────────────────────────────────────

// SimulatePolicyCmd previews policy decisions before promotion.
func SimulatePolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "simulate-policy",
		Short: "Preview policy decisions before promoting a bundle",
		Example: `  sentinelctl simulate-policy \
    --bundle ./policy/bundle.tar.gz \
    --packet ./examples/refund.packet.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			bundle, _ := cmd.Flags().GetString("bundle")
			packet, _ := cmd.Flags().GetString("packet")
			fmt.Printf("Simulating bundle %q against packet %q ...\n", bundle, packet)
			// TODO: POST to /v1/policy/simulate and print SimulationResult.
			fmt.Println("✓ Simulation complete (stub — M2)")
			return nil
		},
	}
	cmd.Flags().String("bundle", "", "Path to proposed policy bundle .tar.gz")
	cmd.Flags().String("packet", "", "Path to packet JSON file")
	return cmd
}

// ─── verify-ledger ───────────────────────────────────────────────────────────

// VerifyLedgerCmd verifies a packet receipt against the chain.
func VerifyLedgerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-ledger",
		Short: "Verify a packet receipt against the governance chain",
		Example: `  sentinelctl verify-ledger --packet-id pkt_01HT...`,
		RunE: func(cmd *cobra.Command, args []string) error {
			packetID, _ := cmd.Flags().GetString("packet-id")
			fmt.Printf("Verifying ledger receipt for packet %q ...\n", packetID)
			// TODO: GET /v1/ledger/receipts/{packet_id} then verify hash.
			fmt.Println("✓ Receipt verified (stub — M4)")
			return nil
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
		Example: `  sentinelctl rewind --correlation-id corr_01HT... --window 72h`,
		RunE: func(cmd *cobra.Command, args []string) error {
			corrID, _ := cmd.Flags().GetString("correlation-id")
			window, _ := cmd.Flags().GetString("window")
			fmt.Printf("Rewinding correlation %q over window %s ...\n", corrID, window)
			// TODO: GET /v1/evidence/rewind/{correlation_id}?window=72h
			fmt.Println("✓ Rewind complete (stub — M3)")
			return nil
		},
	}
	cmd.Flags().String("correlation-id", "", "Correlation ID to rewind")
	cmd.Flags().String("window", "72h", "Evidence window (max 72h for operational mode)")
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
			if profile == "" {
				return fmt.Errorf("--redaction-profile is required")
			}
			fmt.Printf("Exporting evidence for correlation %q with profile %q ...\n", corrID, profile)
			// TODO: POST to /v1/evidence/export and download manifest + bundle.
			fmt.Println("✓ Export complete (stub — M3)")
			return nil
		},
	}
	cmd.Flags().String("correlation-id", "", "Correlation ID to export")
	cmd.Flags().String("redaction-profile", "", "Redaction profile (required)")
	return cmd
}
