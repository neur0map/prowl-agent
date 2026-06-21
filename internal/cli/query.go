package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

// runQuery is the shared core behind every read-only query subcommand. It
// resolves the workspace (walking up from the cwd, like git), freshens the index
// incrementally so the agent never reads stale data, builds a querier, runs fn,
// and prints the result in the requested format.
//
// This is the CLI-first delivery path: an agent shells out to `prowl-agent find
// battery` and gets a cited, token-lean answer. No MCP server, no `serve`, no
// per-client process spawn, and none of MCP's upfront tool-schema token cost.
func runQuery(ctx context.Context, needsAI bool, format outputFormat, w io.Writer, fn func(*query.Querier) (any, error)) error {
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

	// Structural queries never need the model, so they skip the Ollama probe and
	// stay fast. Only semantic search builds an inferencer, and only when AI is
	// enabled; it falls back to full-text when the model is unreachable.
	q := query.New(s)
	if needsAI && cfg.AI.Enabled {
		if oll := maybeInferencer(ctx, cfg); oll != nil {
			q = query.NewWithAssist(s, oll)
			if _, err := reindexer(s, ws.Root, cfg.Ignore, cfg.AI.EmbedModel, oll)(ctx); err != nil {
				return err
			}
		} else {
			if err := freshenStructural(ctx, s, ws.Root, cfg.Ignore); err != nil {
				return err
			}
		}
	} else if err := freshenStructural(ctx, s, ws.Root, cfg.Ignore); err != nil {
		return err
	}

	out, err := fn(q)
	if err != nil {
		return err
	}
	str, err := formatValue(out, format)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, str)
	return err
}

// freshenStructural runs an incremental, structural-only re-index (no embeddings),
// so a query is always current without an Ollama dependency.
func freshenStructural(ctx context.Context, s *store.Store, root string, ignore []string) error {
	_, err := reindexer(s, root, ignore, "", nil)(ctx)
	return err
}

// newQueryCmd builds a thin subcommand that runs one querier method and prints
// the result. Output defaults to TOON (token-lean); --json switches to JSON.
func newQueryCmd(use, short string, needsAI bool, args cobra.PositionalArgs, run func(context.Context, *query.Querier, []string) (any, error)) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE: func(cmd *cobra.Command, a []string) error {
			format := formatTOON
			if asJSON {
				format = formatJSON
			}
			return runQuery(cmd.Context(), needsAI, format, cmd.OutOrStdout(), func(q *query.Querier) (any, error) {
				return run(cmd.Context(), q, a)
			})
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON instead of TOON")
	return c
}

func newFindCmd() *cobra.Command {
	return newQueryCmd("find <name>", "Find symbols (functions, settings, keybinds, components) by name", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.FindSymbol(a[0]) })
}

func newOverviewCmd() *cobra.Command {
	return newQueryCmd("overview", "High-level map of the project (roles, entrypoints, clusters, hotspots)", false, cobra.NoArgs,
		func(_ context.Context, q *query.Querier, _ []string) (any, error) { return q.Overview() })
}

func newClustersCmd() *cobra.Command {
	return newQueryCmd("clusters", "Group related files into subsystems", false, cobra.NoArgs,
		func(_ context.Context, q *query.Querier, _ []string) (any, error) { return q.Clusters() })
}

func newCallersCmd() *cobra.Command {
	return newQueryCmd("callers <path>", "Configs/scripts that include, exec, or bind to a file", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.FindCallers(a[0]) })
}

func newCalleesCmd() *cobra.Command {
	return newQueryCmd("callees <path>", "What a file includes, execs, or binds to", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.FindCallees(a[0]) })
}

func newRelationsCmd() *cobra.Command {
	return newQueryCmd("relations <path>", "A file's defined symbols and include neighbors", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.FileRelations(a[0]) })
}

func newImpactCmd() *cobra.Command {
	return newQueryCmd("impact <path>", "Files that transitively depend on a file (change blast radius)", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.BlastRadius(a[0]) })
}

func newEntrypointsCmd() *cobra.Command {
	return newQueryCmd("entrypoints <path>", "Root files from which a file is reachable", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.EntrypointsFor(a[0]) })
}

func newHotspotsCmd() *cobra.Command {
	return newQueryCmd("hotspots", "Structurally central and large files", false, cobra.NoArgs,
		func(_ context.Context, q *query.Querier, _ []string) (any, error) { return q.RepoHotspots() })
}

func newViolationsCmd() *cobra.Command {
	return newQueryCmd("violations", "Dangling references, orphan scripts, and hardcoded colors", false, cobra.NoArgs,
		func(_ context.Context, q *query.Querier, _ []string) (any, error) { return q.ArchitectureViolations() })
}

func newTestsCmd() *cobra.Command {
	return newQueryCmd("tests <path>", "Configs/keybinds that launch or reload a file (best-effort)", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.TestsFor(a[0]) })
}

func newReferencesCmd() *cobra.Command {
	return newQueryCmd("references <symbol_id>", "Edges pointing at a symbol id (from 'find')", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) {
			id, err := strconv.ParseInt(a[0], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("symbol_id must be an integer (from 'prowl-agent find'): %w", err)
			}
			return q.FindReferences(id)
		})
}

// newSearchCmd is the content/semantic search. It carries extra flags (--smart,
// --compact), so it is not built from the generic helper.
func newSearchCmd() *cobra.Command {
	var asJSON, smart, compact bool
	c := &cobra.Command{
		Use:   "search <query>",
		Short: "Search file content (hybrid semantic+full-text when AI is on, else full-text)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, a []string) error {
			format := formatTOON
			if asJSON {
				format = formatJSON
			}
			text := joinArgs(a)
			return runQuery(cmd.Context(), true, format, cmd.OutOrStdout(), func(q *query.Querier) (any, error) {
				if smart {
					r, err := q.SmartSearch(cmd.Context(), text)
					if err != nil {
						return nil, err
					}
					if compact {
						r.Matches = stripSnippets(r.Matches)
					}
					return r, nil
				}
				m, err := q.SimilarCode(cmd.Context(), text)
				if err != nil {
					return nil, err
				}
				if compact {
					m = stripSnippets(m)
				}
				return m, nil
			})
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON instead of TOON")
	c.Flags().BoolVar(&smart, "smart", false, "rewrite and rerank the query (assist-augmented)")
	c.Flags().BoolVar(&compact, "compact", false, "list files without snippets (most token-lean)")
	return c
}

// stripSnippets drops snippet bodies for token-lean, file-only results.
func stripSnippets(hits []store.ChunkHit) []store.ChunkHit {
	out := make([]store.ChunkHit, len(hits))
	for i, h := range hits {
		h.Snippet = ""
		out[i] = h
	}
	return out
}

func joinArgs(a []string) string {
	s := ""
	for i, w := range a {
		if i > 0 {
			s += " "
		}
		s += w
	}
	return s
}
