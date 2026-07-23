package cli

import (
	"encoding/json"
	"fmt"
	"io"
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
	Tier  string
	// EmbedModel and AssistModel override the tier preset when non-empty. The
	// init command fills them from models already installed on Ollama.
	EmbedModel  string
	AssistModel string
	// Integrations is the explicit set of client/editor integrations to merge.
	// IntegrationsSet distinguishes an intentional empty selection from the
	// legacy programmatic default, which keeps all integrations for API callers.
	Integrations    []string
	IntegrationsSet bool
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
	cfg, err := config.Load(ws.Path)
	if err != nil {
		return index.Summary{}, fmt.Errorf("read existing config: %w", err)
	}
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
	sum, err := index.IndexWithOptions(s, root, index.Options{Ignore: cfg.Ignore, Languages: cfg.Languages})
	if err != nil {
		return sum, err
	}
	integrations := append([]string(nil), allIntegrations...)
	if opt.IntegrationsSet {
		integrations = opt.Integrations
	}
	plan, err := BuildSetupPlan(root, integrations)
	if err != nil {
		return sum, err
	}
	if err := ApplySetupPlan(plan); err != nil {
		return sum, err
	}
	if err := workspace.EnsureDerivedIgnored(root); err != nil {
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
	var withAI, noAI, yes, noInput, reconfigure, dryRun, asJSON, remove bool
	var tier, integrationValue string
	c := &cobra.Command{
		Use:   "init",
		Short: "Plan, preview, and set up Prowl in the current folder",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _ := os.Getwd()
			out := cmd.OutOrStdout()
			nonInteractive := yes || noInput || dryRun || asJSON

			detected := DetectIntegrations(root)
			var integrations []string
			var err error
			if cmd.Flags().Changed("integrations") || nonInteractive {
				integrations, err = ParseIntegrationSelection(integrationValue, detected)
				if err != nil {
					return err
				}
			} else {
				integrations = append([]string(nil), detected...)
				options := make([]huh.Option[string], 0, len(allIntegrations))
				for _, name := range allIntegrations {
					options = append(options, huh.NewOption(name, name).Selected(containsString(detected, name)))
				}
				form := huh.NewForm(huh.NewGroup(
					huh.NewMultiSelect[string]().
						Title("Choose integrations to configure").
						Description("Only selected clients are changed. Existing settings are merged, never replaced.").
						Options(options...).
						Value(&integrations),
				))
				if err := form.Run(); err != nil {
					return err
				}
			}

			plan, err := BuildSetupPlan(root, integrations)
			if err != nil {
				return err
			}
			if dryRun {
				return printSetupPlan(out, plan, asJSON, true)
			}
			if remove {
				if err := RemoveIntegrations(root, integrations); err != nil {
					return err
				}
				if asJSON {
					return json.NewEncoder(out).Encode(map[string]any{"root": root, "removed": integrations})
				}
				fmt.Fprintf(out, "Removed Prowl-owned entries from %d integration(s).\n", len(integrations))
				return nil
			}
			if !asJSON {
				if err := printSetupPlan(out, plan, false, false); err != nil {
					return err
				}
			}

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
				if tier == "" {
					state := "off"
					if ai {
						state = "on"
					}
					uiLog.Infof("using remembered settings (AI %s); pass --reconfigure to change", state)
				}
			}

			// Resolve tier + installed models only when (re)configuring AI; on an
			// inherit, RunInit preserves the project's existing models.
			var embedModel, assistModel string
			if ai && (aiSet || tier != "") {
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

			if !asJSON {
				fmt.Fprintf(out, "Indexing %s ...\n", root)
			}
			sum, err := RunInit(InitOptions{Root: root, AI: ai, AISet: aiSet, Tier: tier, EmbedModel: embedModel, AssistModel: assistModel, Integrations: integrations, IntegrationsSet: true})
			if err != nil {
				return err
			}
			// Run AI setup against the final saved models (resolved or preserved).
			if ai {
				final, _ := config.Load(projDir)
				if tier == "" {
					tier = firstNonEmpty(g.Tier, config.DefaultTier)
				}
				aiOut := io.Writer(out)
				if asJSON {
					aiOut = io.Discard
				}
				setupAI(cmd.Context(), aiOut, config.ModelPreset{Name: tier, EmbedModel: final.AI.EmbedModel, AssistModel: final.AI.AssistModel}, !nonInteractive)
			}
			if asJSON {
				return json.NewEncoder(out).Encode(map[string]any{"root": root, "indexed": sum, "integrations": integrations, "verified": true})
			}
			fmt.Fprintf(out, "Prowl Agent ready: %d files indexed (%d symbols, %d edges).\n", sum.Indexed, sum.Symbols, sum.Edges)
			fmt.Fprintln(out, "Query it from your shell, no server to run:")
			fmt.Fprintln(out, "  prowl-agent overview        a map of this project")
			fmt.Fprintln(out, "  prowl-agent find <name>     locate any symbol")
			fmt.Fprintln(out, "  prowl-agent search <text>   search by meaning or text")
			fmt.Fprintf(out, "%d selected integration(s) configured; .prowl/ is gitignored.\n", len(integrations))
			return nil
		},
	}
	c.Flags().BoolVar(&withAI, "with-ai", false, "enable AI-assist non-interactively")
	c.Flags().BoolVar(&noAI, "no-ai", false, "skip AI-assist non-interactively")
	c.Flags().BoolVar(&yes, "yes", false, "accept defaults without prompting")
	c.Flags().BoolVar(&noInput, "no-input", false, "never prompt (uses detected integrations and remembered settings)")
	c.Flags().BoolVar(&reconfigure, "reconfigure", false, "re-open the AI/tier prompts even if already configured")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "preview exact integration changes without writing anything")
	c.Flags().BoolVar(&asJSON, "json", false, "emit a machine-readable setup plan/report")
	c.Flags().BoolVar(&remove, "remove-integrations", false, "remove only Prowl-owned entries from selected integrations")
	c.Flags().StringVar(&integrationValue, "integrations", "auto", "comma-separated integrations, or auto, none, all")
	c.Flags().StringVar(&tier, "tier", "", "AI model tier: fast, smart, or max")
	c.MarkFlagsMutuallyExclusive("with-ai", "no-ai")
	return c
}

func printSetupPlan(out io.Writer, plan SetupPlan, asJSON, dryRun bool) error {
	if asJSON {
		return json.NewEncoder(out).Encode(map[string]any{"dry_run": dryRun, "plan": plan})
	}
	fmt.Fprintf(out, "Setup plan for %s\n", plan.Root)
	fmt.Fprintln(out, "  • create or refresh the local .prowl workspace and index")
	fmt.Fprintln(out, "  • preserve existing project configuration and rules")
	for _, action := range plan.Actions {
		fmt.Fprintf(out, "  • %-12s %s\n", action.Integration, action.Path)
	}
	if len(plan.Actions) == 0 {
		fmt.Fprintln(out, "  • no client or editor integrations selected")
	}
	if dryRun {
		fmt.Fprintln(out, "Dry run: no files were changed.")
	}
	return nil
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
