// sentinelctl is the Sentinel operator CLI.
//
// Commands:
//   doctor               — connectivity and dependency health check
//   register app         — register a new application
//   emit-test-packet     — emit a test governance packet
//   simulate-policy      — preview policy impact before promotion
//   verify-ledger        — verify a packet receipt against the chain
//   rewind               — reconstruct evidence for a correlation ID
//   export-evidence      — produce a signed evidence export bundle
//
// Run sentinelctl --help for full usage.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/your-org/sentinel/internal/sentinelctl/commands"
)

func main() {
	root := &cobra.Command{
		Use:   "sentinelctl",
		Short: "Sentinel operator CLI",
		Long: `sentinelctl interacts with a running Sentinel control plane.

Set SENTINEL_API_ENDPOINT to point at your sentinel-api instance.
Set SENTINEL_API_TOKEN for authentication.`,
	}

	root.PersistentFlags().String("endpoint", "", "Sentinel API endpoint (overrides SENTINEL_API_ENDPOINT)")
	root.PersistentFlags().String("token", "", "API token (overrides SENTINEL_API_TOKEN)")
	root.PersistentFlags().Bool("json", false, "Output as JSON")

	root.AddCommand(
		commands.DoctorCmd(),
		commands.RegisterCmd(),
		commands.EmitTestPacketCmd(),
		commands.SimulatePolicyCmd(),
		commands.VerifyLedgerCmd(),
		commands.RewindCmd(),
		commands.ExportEvidenceCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
