package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

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

// maybeInferencer returns an Ollama inferencer when AI is enabled and reachable.
func maybeInferencer(ctx context.Context, cfg config.Config) assist.Inferencer {
	if !cfg.AI.Enabled {
		return nil
	}
	oll := assist.NewOllama(cfg.AI.OllamaURL, cfg.AI.EmbedModel, cfg.AI.AssistModel)
	if !oll.Available(ctx) {
		fmt.Fprintf(os.Stderr, "prowl-agent: AI is enabled but Ollama is not reachable at %s; semantic search is off, structural search still works\n", oll.BaseURL)
		return nil
	}
	if !oll.HasModel(ctx, cfg.AI.EmbedModel) {
		fmt.Fprintf(os.Stderr, "prowl-agent: embed model %q is not installed; run 'ollama pull %s'. Semantic search is off, structural search still works\n", cfg.AI.EmbedModel, cfg.AI.EmbedModel)
		return nil
	}
	return oll
}

// reindexer returns a serialized re-index function: structural always, plus
// embeddings when inf is set. Shared by serve and watch.
func reindexer(s *store.Store, root string, ignore []string, embedModel string, inf assist.Inferencer) func(context.Context) (string, error) {
	var mu sync.Mutex
	return func(ctx context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		sum, err := index.Index(s, root, ignore)
		if err != nil {
			return "", err
		}
		msg := fmt.Sprintf("indexed=%d parsed=%d skipped=%d deleted=%d", sum.Indexed, sum.Parsed, sum.Skipped, sum.Deleted)
		if inf != nil {
			// Embeddings are optional: a transient Ollama failure or a missing
			// model must not fail the index. Structural search still works.
			if n, err := index.BuildVectors(ctx, s, inf, embedModel); err != nil {
				msg += fmt.Sprintf(" embed_error=%q", err.Error())
			} else {
				msg += fmt.Sprintf(" embedded=%d", n)
			}
		}
		return msg, nil
	}
}

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

			inf := maybeInferencer(cmd.Context(), cfg)
			_ = s.SetMeta("ai_enabled", strconv.FormatBool(inf != nil))

			reindex := reindexer(s, ws.Root, cfg.Ignore, cfg.AI.EmbedModel, inf)
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
			fresh := newFreshness(cmd.Context(), ws.Root, reindex)
			fresh.start()
			srv := mcpserver.NewServer(q, s, version, reindex, doctorFn, fresh.onCall)
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
