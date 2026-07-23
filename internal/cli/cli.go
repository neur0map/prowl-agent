// Package cli wires Prowl Agent's user-facing commands (init, open, status,
// doctor, update, restart, version) and the hidden agent-launched serve and
// editor-launched lsp commands.
package cli

import "github.com/spf13/cobra"

// Register adds all subcommands to the root command.
func Register(root *cobra.Command, version string) {
	root.AddCommand(newInitCmd(), newOpenCmd(defaultOpenDependencies()), newStatusCmd(version), newDoctorCmd(), newKnowledgeCmd(), newContextCmd(), newCapabilitiesCmd(), newServeCmd(version), newLSPCmd(version), newUpdateCmd(version), newRestartCmd(version), newVersionCmd(version))
	// Read-only query commands: the CLI-first path. Any agent can shell out to
	// these (token-lean TOON output) with no MCP server and no `serve`.
	root.AddCommand(
		newFindCmd(), newSearchCmd(), newOverviewCmd(), newClustersCmd(),
		newCallersCmd(), newCalleesCmd(), newRelationsCmd(), newImpactCmd(),
		newEntrypointsCmd(), newHotspotsCmd(), newViolationsCmd(), newTestsCmd(),
		newReferencesCmd(), newChangedCmd(),
	)
}
