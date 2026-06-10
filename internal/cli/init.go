package cli

import (
	"fmt"
	"os"
	"path/filepath"
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
	// AISet marks AI as an explicit decision (a flag or the interactive prompt).
	// When false, RunInit derives AI from the existing project config, then the
	// global default, so a plain re-init never resets a prior choice.
	AISet bool
	Tier string
	// EmbedModel and AssistModel override the tier preset when non-empty. The
	// init command fills them from models already installed on Ollama.
	EmbedModel  string
	AssistModel string
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

	// Was this project already initialized? A re-init must preserve the saved AI
	// choice rather than reset it (the historic ai=false-on-reinit bug).
	existed := false
	if _, statErr := os.Stat(filepath.Join(ws.Path, "config.toml")); statErr == nil {
		existed = true
	}

	// Base config is the project's existing config when present, else defaults,
	// so a re-init preserves user-edited ignore/languages and the prior AI value.
	cfg, _ := config.Load(ws.Path)
	g, _ := config.LoadGlobal()

	// AI-enable precedence: explicit decision > existing project > global default.
	aiOn := cfg.AI.Enabled
	if !existed {
		aiOn = g.AIEnabled
	}
	if opt.AISet {
		aiOn = opt.AI
	}
	cfg.AI.Enabled = aiOn

	tier := firstNonEmpty(opt.Tier, g.Tier, config.DefaultTier)
	if aiOn {
		switch {
		case opt.Tier != "":
			p := config.PresetByName(opt.Tier)
			cfg.AI.EmbedModel, cfg.AI.AssistModel = p.EmbedModel, p.AssistModel
		case !existed:
			p := config.PresetByName(tier)
			cfg.AI.EmbedModel = firstNonEmpty(g.EmbedModel, p.EmbedModel)
			cfg.AI.AssistModel = firstNonEmpty(g.AssistModel, p.AssistModel)
		}
		if opt.EmbedModel != "" {
			cfg.AI.EmbedModel = opt.EmbedModel
		}
		if opt.AssistModel != "" {
			cfg.AI.AssistModel = opt.AssistModel
		}
	}

	if err := config.Save(ws.Path, cfg); err != nil {
		return index.Summary{}, err
	}
	// Remember the choice binary-wide so future inits inherit it, but only on an
	// explicit decision or a brand-new project: a plain re-index of an existing
	// project must not silently change the global default.
	if opt.AISet || !existed {
		_ = config.SaveGlobal(config.GlobalConfig{
			AIEnabled:   aiOn,
			Tier:        tier,
			EmbedModel:  cfg.AI.EmbedModel,
			AssistModel: cfg.AI.AssistModel,
		})
	}

	// Write starter rules only when absent, so a re-init keeps user-edited rules.
	if _, statErr := os.Stat(filepath.Join(ws.Path, "rules.toml")); os.IsNotExist(statErr) {
		if err := config.SaveRules(ws.Path, config.DefaultRules()); err != nil {
			return index.Summary{}, err
		}
	}
	s, err := store.Open(ws.DB)
	if err != nil {
		return index.Summary{}, err
	}
	defer s.Close()
	_ = s.SetMeta("ai_enabled", strconv.FormatBool(aiOn))
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
	if err := workspace.Register(root, aiOn); err != nil {
		return sum, err
	}
	return sum, nil
}

// firstNonEmpty returns the first non-empty string, or "" when all are empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func newInitCmd() *cobra.Command {
	var withAI, noAI, yes, reconfigure bool
	var tier string
	c := &cobra.Command{
		Use:   "init",
		Short: "Set up Prowl Agent in the current folder (interactive wizard)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _ := os.Getwd()
			out := cmd.OutOrStdout()

			// What do we already know? A project config and/or a remembered global
			// default mean we should not re-prompt unless --reconfigure is passed.
			projDir := filepath.Join(root, workspace.Dir)
			projInit := false
			if _, e := os.Stat(filepath.Join(projDir, "config.toml")); e == nil {
				projInit = true
			}
			g, _ := config.LoadGlobal()
			remembered := projInit || config.GlobalExists()

			// Inherited AI value by precedence: existing project, else global.
			inheritedAI := g.AIEnabled
			if projInit {
				if pc, e := config.Load(projDir); e == nil {
					inheritedAI = pc.AI.Enabled
				}
			}

			var ai, aiSet bool
			switch {
			case withAI:
				ai, aiSet = true, true
			case noAI:
				ai, aiSet = false, true
			case yes:
				ai, aiSet = inheritedAI, false
			case reconfigure || !remembered:
				ai = inheritedAI // seed the toggle with the current value
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
				aiSet = true
			default:
				ai, aiSet = inheritedAI, false
				state := "off"
				if ai {
					state = "on"
				}
				uiLog.Infof("using remembered settings (AI %s); pass --reconfigure to change", state)
			}

			// Resolve tier + installed models only when (re)configuring AI; on an
			// inherit, RunInit preserves the project's existing models.
			var embedModel, assistModel string
			if ai && aiSet {
				if tier == "" {
					tier = firstNonEmpty(g.Tier, config.DefaultTier)
					if !yes && (reconfigure || !remembered) {
						tier = selectTier()
					}
				}
				p := config.PresetByName(tier)
				oll := assist.NewOllama("", p.EmbedModel, p.AssistModel)
				embedModel, assistModel = resolveModels(cmd.Context(), oll, p)
			}

			fmt.Fprintf(out, "Indexing %s ...\n", root)
			sum, err := RunInit(InitOptions{Root: root, AI: ai, AISet: aiSet, Tier: tier, EmbedModel: embedModel, AssistModel: assistModel})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Prowl Agent ready: %d files indexed (%d symbols, %d edges).\n", sum.Indexed, sum.Symbols, sum.Edges)
			fmt.Fprintln(out, "Registered MCP server in .mcp.json and instructions in AGENTS.md; .prowl/ is gitignored.")
			fmt.Fprintln(out, "Editor LSP: configured. Your editor launches 'prowl-agent lsp' automatically; see .prowl/editor/SETUP.md.")

			// Run AI setup against the final saved models (resolved or preserved).
			if ai {
				final, _ := config.Load(projDir)
				if tier == "" {
					tier = firstNonEmpty(g.Tier, config.DefaultTier)
				}
				setupAI(cmd.Context(), out, config.ModelPreset{Name: tier, EmbedModel: final.AI.EmbedModel, AssistModel: final.AI.AssistModel}, !yes)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&withAI, "with-ai", false, "enable AI-assist non-interactively")
	c.Flags().BoolVar(&noAI, "no-ai", false, "skip AI-assist non-interactively")
	c.Flags().BoolVar(&yes, "yes", false, "accept defaults without prompting")
	c.Flags().BoolVar(&reconfigure, "reconfigure", false, "re-open the AI/tier prompts even if already configured")
	c.Flags().StringVar(&tier, "tier", "", "AI model tier: fast, smart, or max")
	return c
}
