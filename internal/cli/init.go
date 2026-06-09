package cli

import (
	"fmt"
	"os"
	"strconv"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/assist"
	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/index"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

// InitOptions controls a non-interactive init.
type InitOptions struct {
	Root string
	AI   bool
}

// RunInit creates the workspace, writes config/rules, runs the first index,
// injects agent config, wires .gitignore, and registers the project. It is the
// testable core behind the interactive `init` command.
func RunInit(opt InitOptions) (index.Summary, error) {
	root := opt.Root
	if root == "" {
		root, _ = os.Getwd()
	}
	ws, err := workspace.Create(root)
	if err != nil {
		return index.Summary{}, err
	}
	cfg := config.Default()
	cfg.AI.Enabled = opt.AI
	if err := config.Save(ws.Path, cfg); err != nil {
		return index.Summary{}, err
	}
	if err := config.SaveRules(ws.Path, config.DefaultRules()); err != nil {
		return index.Summary{}, err
	}
	s, err := store.Open(ws.DB)
	if err != nil {
		return index.Summary{}, err
	}
	defer s.Close()
	_ = s.SetMeta("ai_enabled", strconv.FormatBool(opt.AI))
	sum, err := index.Index(s, root, cfg.Ignore)
	if err != nil {
		return sum, err
	}
	if err := Inject(root); err != nil {
		return sum, err
	}
	if err := workspace.EnsureIgnored(root, workspace.Dir+"/"); err != nil {
		return sum, err
	}
	if err := workspace.Register(root, opt.AI); err != nil {
		return sum, err
	}
	return sum, nil
}

func newInitCmd() *cobra.Command {
	var withAI, noAI, yes bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Set up Prowl Agent in the current folder (interactive wizard)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _ := os.Getwd()
			out := cmd.OutOrStdout()
			ai := withAI
			if !yes && !withAI && !noAI {
				form := huh.NewForm(huh.NewGroup(
					huh.NewConfirm().
						Title("Enable AI-assisted semantic search?").
						Description("Adds fuzzy/semantic search powered by a small local model (via Ollama).\n" +
							"Structural search works without it; you can enable this later.").
						Affirmative("Enable").
						Negative("Skip").
						Value(&ai),
				))
				if err := form.Run(); err != nil {
					return err
				}
			}
			if noAI {
				ai = false
			}
			fmt.Fprintf(out, "Indexing %s ...\n", root)
			sum, err := RunInit(InitOptions{Root: root, AI: ai})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Prowl Agent ready: %d files indexed (%d symbols, %d edges).\n", sum.Indexed, sum.Symbols, sum.Edges)
			fmt.Fprintln(out, "Registered MCP server in .mcp.json and instructions in AGENTS.md; .prowl/ is gitignored.")
			if ai {
				cfg := config.Default()
				fmt.Fprintln(out, "AI-assist enabled. Default models:")
				fmt.Fprintf(out, "  embed: %s  rerank: %s  assist: %s\n", cfg.AI.EmbedModel, cfg.AI.RerankModel, cfg.AI.AssistModel)
				oll := assist.NewOllama(cfg.AI.OllamaURL, cfg.AI.EmbedModel, cfg.AI.AssistModel)
				if oll.Available(cmd.Context()) {
					fmt.Fprintf(out, "  Ollama detected at %s. Pull models with: ollama pull %s\n", cfg.AI.OllamaURL, cfg.AI.EmbedModel)
				} else {
					fmt.Fprintf(out, "  Ollama not detected at %s. Install it (e.g. 'pacman -S ollama') to enable semantic search.\n", cfg.AI.OllamaURL)
				}
				fmt.Fprintln(out, "  (Semantic index builds in an upcoming release.)")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&withAI, "with-ai", false, "enable AI-assist non-interactively")
	c.Flags().BoolVar(&noAI, "no-ai", false, "skip AI-assist non-interactively")
	c.Flags().BoolVar(&yes, "yes", false, "accept defaults without prompting")
	return c
}
