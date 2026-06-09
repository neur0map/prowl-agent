package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/assist"
	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/doctor"
	"github.com/prowl-agent/prowl-agent/internal/index"
	mcpserver "github.com/prowl-agent/prowl-agent/internal/mcp"
	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

// newServeCmd is hidden: agents launch it via the injected .mcp.json.
func newServeCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:    "serve",
		Short:  "Run the MCP server over stdio (launched by coding agents)",
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

			var inf assist.Inferencer
			if cfg.AI.Enabled {
				oll := assist.NewOllama(cfg.AI.OllamaURL, cfg.AI.EmbedModel, cfg.AI.AssistModel)
				if oll.Available(cmd.Context()) {
					inf = oll
				}
			}

			reindex := func(ctx context.Context) (string, error) {
				sum, err := index.Index(s, ws.Root, cfg.Ignore)
				if err != nil {
					return "", err
				}
				msg := fmt.Sprintf("indexed=%d parsed=%d skipped=%d deleted=%d",
					sum.Indexed, sum.Parsed, sum.Skipped, sum.Deleted)
				if inf != nil {
					n, err := index.BuildVectors(ctx, s, inf, cfg.AI.EmbedModel)
					if err != nil {
						return msg, err
					}
					msg += fmt.Sprintf(" embedded=%d", n)
				}
				return msg, nil
			}
			// Freshen the index on startup (incremental, so cheap after first run).
			if _, err := reindex(cmd.Context()); err != nil {
				return err
			}
			q := query.New(s)
			if inf != nil {
				q = query.NewWithAssist(s, inf)
			}
			doctorFn := func(context.Context) (doctor.Report, error) {
				rules, _ := config.LoadRules(ws.Path)
				return doctor.Run(s, rules, doctor.Options{Root: ws.Root})
			}
			srv := mcpserver.NewServer(q, version, reindex, doctorFn)
			// A clean client disconnect surfaces as EOF / "closing"; treat it as success.
			if err := mcpserver.Serve(cmd.Context(), srv); err != nil &&
				!errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) &&
				!strings.Contains(err.Error(), "closing") {
				return err
			}
			return nil
		},
	}
}
