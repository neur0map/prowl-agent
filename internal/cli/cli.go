// Package cli wires Prowl Agent's user-facing commands (init, status, doctor)
// and the hidden agent-launched serve and editor-launched lsp commands.
package cli

import "github.com/spf13/cobra"

// Register adds all subcommands to the root command.
func Register(root *cobra.Command, version string) {
	root.AddCommand(newInitCmd(), newStatusCmd(version), newDoctorCmd(), newServeCmd(version), newLSPCmd(version), newWatchCmd(version), newUpdateCmd(version))
}
