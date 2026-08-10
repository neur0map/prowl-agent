// Package cli wires Prowl Agent's user-facing commands (init, status, doctor,
// update, restart, version), the hidden agent-launched serve command, the
// editor-launched lsp command, and the read-only query commands. Prowl is an
// agent-first context engine: the CLI and MCP surfaces are the whole product.
package cli

import "github.com/spf13/cobra"

// Register adds all subcommands to the root command.
func Register(root *cobra.Command, version string) {
	root.AddCommand(newInitCmd(), newStatusCmd(version), newDoctorCmd(), newKnowledgeCmd(), newContextCmd(), newCapabilitiesCmd(), newServeCmd(version), newLSPCmd(version), newUpdateCmd(version), newRestartCmd(version), newVersionCmd(version))
	// Read-only query commands: the CLI-first path. Any agent can shell out to
	// these (token-lean TOON output) with no MCP server and no `serve`.
	root.AddCommand(
		newFindCmd(), newDefCmd(), newOutlineCmd(), newSearchCmd(), newOverviewCmd(), newClustersCmd(),
		newCallersCmd(), newCalleesCmd(), newRelationsCmd(), newImpactCmd(),
		newEntrypointsCmd(), newHotspotsCmd(), newViolationsCmd(), newTestsCmd(),
		newReferencesCmd(), newChangedCmd(), newWipCmd(), newExploreCmd(),
		newBriefCmd(), newDocsCmd(), newSketchCmd(), newGraphCmd(),
	)
}
