package cli

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/application"
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
func runQuery(ctx context.Context, needsAI bool, format outputFormat, limit int, w io.Writer, fn func(*query.Querier) (any, error)) error {
	q, _, s, closer, err := openQuerier(ctx, needsAI)
	if err != nil {
		return err
	}
	defer closer()
	out, err := fn(q)
	if err != nil {
		return err
	}
	// --limit caps a top-level result slice, so an agent can ask for the top N and
	// pay for fewer tokens. The cap is applied before stats and output, so both
	// reflect what was actually returned.
	out = capSlice(out, limit)
	// Count this answer toward the savings report, so 'prowl-agent status'
	// reflects shell usage, not just MCP. Never fail a query over a stat write.
	_ = s.RecordAnswer(out)
	str, err := formatValue(out, format)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, str)
	return err
}

// capSlice truncates a top-level result slice to limit (limit <= 0 means no cap).
// Struct results (overview, relations) are returned unchanged.
func capSlice(out any, limit int) any {
	if limit <= 0 {
		return out
	}
	rv := reflect.ValueOf(out)
	if rv.Kind() == reflect.Slice && rv.Len() > limit {
		return rv.Slice(0, limit).Interface()
	}
	return out
}

// openQuerier resolves the workspace, freshens the index incrementally, and
// builds a querier. It is shared by every read-only command. The returned closer
// must be called to release the store. needsAI adds the semantic layer when AI is
// enabled and reachable (and re-embeds during the refresh); structural commands
// pass false to skip the Ollama probe and stay fast.
func openQuerier(ctx context.Context, needsAI bool) (*query.Querier, *workspace.Workspace, *store.Store, func() error, error) {
	project, err := application.OpenProject(ctx, ".", application.Options{
		EnableAI: needsAI, InferencerProvider: maybeInferencer,
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return project.Query, project.Workspace, project.Store, project.Close, nil
}

// newQueryCmd builds a thin subcommand that runs one querier method and prints
// the result. Humans get a readable TTY view; pipes keep token-lean TOON.
func newQueryCmd(use, short string, needsAI bool, args cobra.PositionalArgs, run func(context.Context, *query.Querier, []string) (any, error)) *cobra.Command {
	var output outputOptions
	var limit int
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE: func(cmd *cobra.Command, a []string) error {
			format, err := output.resolve(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			return runQuery(cmd.Context(), needsAI, format, limit, cmd.OutOrStdout(), func(q *query.Querier) (any, error) {
				return run(cmd.Context(), q, a)
			})
		},
	}
	output.addFlags(c)
	c.Flags().IntVar(&limit, "limit", 0, "cap results to N (fewer tokens; 0 = default)")
	return c
}

func newFindCmd() *cobra.Command {
	return newQueryCmd("find <name>", "Find symbols (functions, settings, keybinds, components) by name", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.FindSymbol(a[0]) })
}

func newOutlineCmd() *cobra.Command {
	return newQueryCmd("outline <path>", "Show a file's structure: its symbols, signatures, and line ranges (no bodies), to grasp a file without reading it", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.Outline(a[0]) })
}

func newOverviewCmd() *cobra.Command {
	return newQueryCmd("overview", "High-level map of the project (roles, entrypoints, clusters, hotspots)", false, cobra.NoArgs,
		func(_ context.Context, q *query.Querier, _ []string) (any, error) { return q.Overview() })
}

func newBriefCmd() *cobra.Command {
	return newQueryCmd("brief <path>", "Cited orientation for a path or subsystem: size, languages, guides, and the key files to read first (warm-start a subagent in one call)", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.Brief(a[0]) })
}

// newClustersCmd lists subsystem summaries by default (label, language, file
// count); given a name it returns the full file list of matching subsystems, so
// "pull a whole subsystem" stays cheap instead of dumping every cluster's files.
func newClustersCmd() *cobra.Command {
	var output outputOptions
	c := &cobra.Command{
		Use:   "clusters [subsystem]",
		Short: "Project subsystems (summaries); with a name, the files in matching subsystems",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, a []string) error {
			format, err := output.resolve(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			return runQuery(cmd.Context(), false, format, 0, cmd.OutOrStdout(), func(q *query.Querier) (any, error) {
				clusters, err := q.Clusters()
				if err != nil {
					return nil, err
				}
				if len(a) == 0 {
					out := make([]query.ClusterSummary, len(clusters))
					for i, c := range clusters {
						out[i] = query.ClusterSummary{Label: c.Label, Lang: c.Lang, Files: len(c.Files)}
					}
					return out, nil
				}
				needle := strings.ToLower(a[0])
				out := make([]query.Cluster, 0, len(clusters))
				for _, c := range clusters {
					if strings.Contains(strings.ToLower(c.Label), needle) {
						out = append(out, c)
					}
				}
				return out, nil
			})
		},
	}
	output.addFlags(c)
	return c
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

// newImpactCmd summarizes the blast radius by default (count + by-subsystem +
// direct importers) to stay token-lean on large graphs; --all dumps every
// dependent file.
func newImpactCmd() *cobra.Command {
	var output outputOptions
	var all bool
	var limit int
	c := &cobra.Command{
		Use:   "impact <path>",
		Short: "Blast radius of a file: dependent count, subsystems, and direct importers",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, a []string) error {
			format, err := output.resolve(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			return runQuery(cmd.Context(), false, format, limit, cmd.OutOrStdout(), func(q *query.Querier) (any, error) {
				if all {
					return q.BlastRadius(a[0])
				}
				return q.BlastSummarize(a[0])
			})
		},
	}
	output.addFlags(c)
	c.Flags().BoolVar(&all, "all", false, "list every dependent file instead of a summary")
	c.Flags().IntVar(&limit, "limit", 0, "cap results to N (fewer tokens; 0 = default)")
	return c
}

func newEntrypointsCmd() *cobra.Command {
	return newQueryCmd("entrypoints <path>", "Root files from which a file is reachable", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.EntrypointsFor(a[0]) })
}

func newHotspotsCmd() *cobra.Command {
	return newQueryCmd("hotspots", "Central files, largest files, largest functions, and most complex functions", false, cobra.NoArgs,
		func(_ context.Context, q *query.Querier, _ []string) (any, error) { return q.RepoHotspots() })
}

func newViolationsCmd() *cobra.Command {
	return newQueryCmd("violations", "Dangling references, orphan scripts, and hardcoded colors", false, cobra.NoArgs,
		func(_ context.Context, q *query.Querier, _ []string) (any, error) { return q.ArchitectureViolations() })
}

func newTestsCmd() *cobra.Command {
	return newQueryCmd("tests <path>", "Test files covering a file (colocated or importing it), or for a config what launches it", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.TestsFor(a[0]) })
}

func newReferencesCmd() *cobra.Command {
	return newQueryCmd("references <name-or-id>", "Where a symbol is used: reference edges, or cited call sites for code, resolved by name or by an id from 'find'", false, cobra.ExactArgs(1),
		func(_ context.Context, q *query.Querier, a []string) (any, error) { return q.References(a[0]) })
}

// newSearchCmd is the content/semantic search. It carries extra flags (--smart,
// --compact), so it is not built from the generic helper.
func newSearchCmd() *cobra.Command {
	var output outputOptions
	var smart, compact bool
	var limit int
	c := &cobra.Command{
		Use:   "search <query>",
		Short: "Search file content (hybrid semantic+full-text when AI is on, else full-text)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, a []string) error {
			format, err := output.resolve(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			text := joinArgs(a)
			return runQuery(cmd.Context(), true, format, limit, cmd.OutOrStdout(), func(q *query.Querier) (any, error) {
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
	output.addFlags(c)
	c.Flags().BoolVar(&smart, "smart", false, "rewrite and rerank the query (assist-augmented)")
	c.Flags().BoolVar(&compact, "compact", false, "list files without snippets (most token-lean)")
	c.Flags().IntVar(&limit, "limit", 0, "cap results to N (fewer tokens; 0 = default)")
	return c
}

// newDefCmd shows a symbol's source. It resolves the symbol like `find`, then
// returns only its body (bounded, cited), so an agent reads one function or
// component instead of the whole file. It needs the workspace root to read the
// source, so it is not built from the generic helper.
func newDefCmd() *cobra.Command {
	var output outputOptions
	c := &cobra.Command{
		Use:   "def <name-or-id>",
		Short: "Show a symbol's source (signature and body), cited and bounded, so you read one symbol not the whole file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, a []string) error {
			format, err := output.resolve(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			q, ws, s, closer, err := openQuerier(cmd.Context(), false)
			if err != nil {
				return err
			}
			defer closer()
			def, err := q.Definition(ws.Root, a[0])
			if err != nil {
				return err
			}
			_ = s.RecordAnswer(def)
			str, err := formatValue(def, format)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), str)
			return err
		},
	}
	output.addFlags(c)
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
