package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/application"
	"github.com/prowl-agent/prowl-agent/internal/assist"
	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/doctor"
	"github.com/prowl-agent/prowl-agent/internal/embed"
	mcpserver "github.com/prowl-agent/prowl-agent/internal/mcp"
)

// loadEmbedder loads the in-process, binary-bundled static embedder. It is a
// package variable so tests can stub the embedding backend.
var loadEmbedder = func(context.Context) (assist.Embedder, error) {
	m, err := embed.Load()
	if err != nil {
		return nil, err
	}
	return m, nil
}

// maybeInferencer resolves the semantic-assist backend. AI is always on, and
// embeddings are always available: a local Ollama embed model when the user has
// one (highest quality, and an all-in-one embed+generate+rerank backend),
// otherwise the in-process static embedder (internal/embed) -- no daemon, no API
// key -- so semantic vector search works in every repo with zero setup.
// Generation and rerank (the --smart half) come from Ollama, else a coding-agent
// CLI, else nothing (vector+FTS still works). Only a total embedder failure
// (e.g. an offline first run with no cache) falls back to agent-only or
// structural search.
func maybeInferencer(ctx context.Context, cfg config.Config) assist.Inferencer {
	if !cfg.AI.Enabled {
		return nil
	}
	// Best case: a local Ollama embed model -- higher quality than the static
	// model and all-in-one (embed + generate + rerank).
	if cfg.AI.Provider != "agent" {
		oll := assist.NewOllama(cfg.AI.OllamaURL, cfg.AI.EmbedModel, cfg.AI.AssistModel)
		if oll.Available(ctx) && oll.HasModel(ctx, cfg.AI.EmbedModel) {
			return oll
		}
	}
	// Optional coding-agent CLI for the generate/rerank half.
	var agent assist.Inferencer
	cmd := cfg.AI.AgentCommand
	if cmd == "" {
		cmd = detectAgentCLI()
	}
	if cmd != "" {
		if a := assist.NewAgentCLI(cmd); a.Available(ctx) {
			agent = a
		}
	}
	// Guaranteed path: in-process static embeddings for vector search, paired
	// with the agent (if any) for rewrite + rerank.
	if m, err := loadEmbedder(ctx); err == nil {
		return assist.Composite{Emb: m, Assist: agent}
	} else if agent != nil {
		fmt.Fprintf(os.Stderr, "prowl-agent: built-in embedder unavailable (%v); using coding-agent rerank without embeddings\n", err)
		return agent
	} else {
		fmt.Fprintf(os.Stderr, "prowl-agent: built-in embedder unavailable (%v); structural search only\n", err)
		return nil
	}
}

// reindexer formats the shared Project refresh operation for MCP and restart.
func reindexer(project *application.Project) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		result, err := project.Refresh(ctx)
		if err != nil {
			return "", err
		}
		sum := result.Summary
		msg := fmt.Sprintf("indexed=%d parsed=%d skipped=%d deleted=%d", sum.Indexed, sum.Parsed, sum.Skipped, sum.Deleted)
		if result.EmbeddingError != nil {
			msg += fmt.Sprintf(" embed_error=%q", result.EmbeddingError.Error())
		} else if project.Inferencer != nil {
			msg += fmt.Sprintf(" embedded=%d", result.Embedded)
		}
		return msg, nil
	}
}

func openServeProject(ctx context.Context) (*application.Project, error) {
	return application.OpenProject(ctx, ".", application.Options{EnableAI: true, InferencerProvider: maybeInferencer})
}

// newServeCmd is hidden: agents launch it via the injected .mcp.json.
func newServeCmd(version string) *cobra.Command {
	var surfaceName string
	command := &cobra.Command{
		Use:    "serve",
		Short:  "Run the MCP server over stdio (launched by coding agents)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			surface, err := mcpserver.ParseSurface(surfaceName)
			if err != nil {
				return err
			}
			project, err := openServeProject(cmd.Context())
			if err != nil {
				return err
			}
			defer project.Close()
			reindex := reindexer(project)
			doctorFn := func(ctx context.Context) (doctor.Report, error) {
				release, err := project.ReadGuard(ctx)
				if err != nil {
					return doctor.Report{}, err
				}
				defer release()
				if err := project.Store.RequirePublishedGeneration(); err != nil {
					return doctor.Report{}, err
				}
				rules, err := config.LoadRules(project.Workspace.Path)
				if err != nil {
					return doctor.Report{}, err
				}
				return doctor.Run(project.Store, rules, doctor.Options{Root: project.Workspace.Root})
			}
			fresh := newFreshness(cmd.Context(), project.Workspace.Root, reindex)
			fresh.start()
			defer fresh.stop()
			srv := mcpserver.NewServerWithOptions(project.Query, project.Store, version, reindex, doctorFn, nil, mcpserver.ServerOptions{
				Surface: surface, Context: project.Context, Knowledge: project.Knowledge, Capabilities: project.Capabilities, Root: project.Workspace.Root,
				BeforeCall: fresh.onCall,
			})
			// A clean client disconnect surfaces as EOF / "closing"; treat it as success.
			if err := mcpserver.Serve(cmd.Context(), srv); err != nil &&
				!errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) &&
				!strings.Contains(err.Error(), "closing") {
				return err
			}
			return nil
		},
	}
	command.Flags().StringVar(&surfaceName, "mcp-surface", string(mcpserver.SurfaceLegacy), "MCP tool surface: legacy, core, or all")
	return command
}
