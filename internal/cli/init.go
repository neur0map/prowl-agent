package cli

import (
	"fmt"
	"os"
	"strconv"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/index"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

// InitOptions controls a non-interactive init.
type InitOptions struct {
	Root string
	AI   bool
	Tier string
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
	if opt.AI {
		p := config.PresetByName(opt.Tier)
		cfg.AI.EmbedModel, cfg.AI.AssistModel = p.EmbedModel, p.AssistModel
	}
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
	if err := InjectEditor(root); err != nil {
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
	var tier string
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
			if ai && tier == "" {
				tier = config.DefaultTier
				if !yes {
					tier = selectTier()
				}
			}
			fmt.Fprintf(out, "Indexing %s ...\n", root)
			sum, err := RunInit(InitOptions{Root: root, AI: ai, Tier: tier})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Prowl Agent ready: %d files indexed (%d symbols, %d edges).\n", sum.Indexed, sum.Symbols, sum.Edges)
			fmt.Fprintln(out, "Registered MCP server in .mcp.json and instructions in AGENTS.md; .prowl/ is gitignored.")
			fmt.Fprintln(out, "Editor LSP: configured. Your editor launches 'prowl-agent lsp' automatically; see .prowl/editor/SETUP.md.")
			if ai {
				setupAI(cmd.Context(), out, config.PresetByName(tier), !yes)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&withAI, "with-ai", false, "enable AI-assist non-interactively")
	c.Flags().BoolVar(&noAI, "no-ai", false, "skip AI-assist non-interactively")
	c.Flags().BoolVar(&yes, "yes", false, "accept defaults without prompting")
	c.Flags().StringVar(&tier, "tier", "", "AI model tier: fast, smart, or max")
	return c
}
