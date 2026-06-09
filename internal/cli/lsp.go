package cli

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/index"
	lspserver "github.com/prowl-agent/prowl-agent/internal/lsp"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

// newLSPCmd is hidden: editors launch it via the config that init writes. It
// serves the same per-project index as `serve`, but over LSP for a human editor.
func newLSPCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:    "lsp",
		Short:  "Run the language server over stdio (launched by your editor)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := workspace.Resolve(".")
			if err != nil {
				return err
			}
			s, err := store.Open(ws.DB)
			if err != nil {
				return err
			}
			defer s.Close()
			cfg, _ := config.Load(ws.Path)
			rules, _ := config.LoadRules(ws.Path)

			reindex := func() error {
				_, err := index.Index(s, ws.Root, cfg.Ignore)
				return err
			}
			// Freshen on startup (incremental, cheap after the first run).
			_ = reindex()

			srv := lspserver.New(ws.Root, version, s, rules, reindex)
			// External edits (agent, git, formatter): reindex and refresh squiggles.
			go func() {
				_ = index.Watch(cmd.Context(), ws.Root, 750*time.Millisecond, func() {
					if reindex() == nil {
						srv.RepublishOpen()
					}
				})
			}()
			return srv.Run(cmd.Context(), os.Stdin, os.Stdout)
		},
	}
}
