// Package cli wires Prowl Agent's user-facing commands (init, status) and the
// hidden agent-launched serve command onto the cobra root.
package cli

import "github.com/spf13/cobra"

// Register adds all subcommands to the root command.
func Register(root *cobra.Command, version string) {
	root.AddCommand(newInitCmd(), newStatusCmd(), newServeCmd(version))
}
