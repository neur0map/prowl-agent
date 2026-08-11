package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/application"
	"github.com/prowl-agent/prowl-agent/internal/assist"
	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/doctor"
	"github.com/prowl-agent/prowl-agent/internal/index"
	"github.com/prowl-agent/prowl-agent/internal/parse"
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
	// Provider/AgentCommand select the semantic-assist backend. Provider
	// "agent" borrows a coding-agent CLI (AgentCommand, e.g. "claude -p") for
	// reranking instead of a local Ollama model.
	Provider     string
	AgentCommand string
	// Integrations is the explicit set of client/editor integrations to merge.
	// IntegrationsSet distinguishes an intentional empty selection from the
	// legacy programmatic default, which keeps all integrations for API callers.
	Integrations    []string
	IntegrationsSet bool
	// Languages overrides the config language filter at init when LanguagesSet.
	// It gives a one-command fix for the silent-empty-index case (a copied config
	// excluding the repo's real stack) that the post-init warning points at.
	Languages    []string
	LanguagesSet bool
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
		if opt.Provider != "" {
			cfg.AI.Provider = opt.Provider
		}
		if opt.AgentCommand != "" {
			cfg.AI.AgentCommand = opt.AgentCommand
		}
	}
	if opt.LanguagesSet {
		cfg.Languages = opt.Languages
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
	project, err := application.OpenProject(context.Background(), root, application.Options{})
	if err != nil {
		return index.Summary{}, err
	}
	defer project.Close()
	sum := project.InitialRefresh.Summary
	if sum.Indexed == 0 {
		// A current re-init reports the existing index totals (files, symbols,
		// edges) without forcing another mutation pass, so the summary reflects
		// what is indexed rather than an empty no-change delta.
		if status, statusErr := project.Query.Status(); statusErr == nil {
			sum.Indexed = status.Counts.Files
			sum.Symbols = status.Counts.Symbols
			sum.Edges = status.Counts.Edges
		}
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
	// Seed the always-on Prowl map now that setup has written the AGENTS.md
	// guidance block; best-effort, never fails init.
	if ov, ovErr := project.Query.Overview(); ovErr == nil {
		_ = refreshAgentsMap(root, ov)
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

// unindexedLanguageWarnings reports languages present on disk but excluded from
// the index by the config `languages` filter. init and status surface these so a
// silently near-empty index is caught immediately, not discovered as empty query
// results later.
func unindexedLanguageWarnings(root string) []string {
	ws, err := workspace.Resolve(root)
	if err != nil {
		return nil
	}
	cfg, err := config.Load(ws.Path)
	if err != nil {
		return nil
	}
	s, err := store.Open(ws.DB)
	if err != nil {
		return nil
	}
	defer s.Close()
	return doctor.UnindexedLanguageWarnings(s, root, cfg.Ignore)
}

// shouldHealLanguages reports the language list init should use when the existing
// config's filter is almost certainly stale -- it excludes more of the repo's
// code than it includes (e.g. a rice/QML config copied into a Go project). In
// that case indexing defaults back to auto so init "just works"; a deliberately
// narrow filter (which keeps the majority of the code) is left untouched.
func shouldHealLanguages(root string) ([]string, bool) {
	ws, err := workspace.Resolve(root)
	if err != nil {
		return nil, false
	}
	cfg, err := config.Load(ws.Path)
	if err != nil {
		return nil, false
	}
	if languageFilterMostlyExcludes(root, cfg.Ignore, cfg.Languages) {
		return []string{"auto"}, true
	}
	return nil, false
}

// languageFilterMostlyExcludes reports whether a non-auto languages filter would
// leave more on-disk code files unindexed than indexed.
func languageFilterMostlyExcludes(root string, ignore, languages []string) bool {
	if len(languages) == 0 {
		return false
	}
	allow := make(map[string]bool, len(languages))
	for _, l := range languages {
		if l == "auto" {
			return false
		}
		allow[l] = true
	}
	rels, err := index.WalkContext(context.Background(), root, ignore)
	if err != nil {
		return false
	}
	var allowed, excluded int
	for _, rel := range rels {
		lang := parse.Detect(rel, nil)
		if lang == "" || !parse.HasGrammar(lang) {
			continue
		}
		if allow[lang] {
			allowed++
		} else {
			excluded++
		}
	}
	return excluded > allowed && excluded >= 10
}

func newInitCmd() *cobra.Command {
	var withAI, noAI, yes, noInput, reconfigure, dryRun, asJSON, remove bool
	var tier, integrationValue, languagesValue, aiProvider, aiCommand string
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

			// Decide the semantic-assist backend. --ai-provider wins; otherwise
			// inherit the project's saved provider. When enabling AI without a
			// reachable local model, fall back to an installed coding-agent CLI
			// so reranking still works with no daemon.
			provider, agentCommand := aiProvider, aiCommand
			if provider == "" {
				if pc, e := config.Load(projDir); e == nil {
					provider = pc.AI.Provider
					if agentCommand == "" {
						agentCommand = pc.AI.AgentCommand
					}
				}
			}
			var embedModel, assistModel string
			if ai && provider != "agent" && (aiSet || tier != "") {
				if tier == "" {
					tier = firstNonEmpty(g.Tier, config.DefaultTier)
					if !yes && (reconfigure || !remembered) {
						tier = selectTier()
					}
				}
				p := config.PresetByName(tier)
				oll := assist.NewOllama("", p.EmbedModel, p.AssistModel)
				if provider == "" && !oll.Available(cmd.Context()) {
					if detected := detectAgentCLI(); detected != "" {
						provider, agentCommand = "agent", detected
						uiLog.Infof("no local model reachable; reranking via coding-agent CLI %q (cheap tier). Override with --ai-command", agentCommand)
					}
				}
				if provider != "agent" {
					embedModel, assistModel = resolveModels(cmd.Context(), oll, p)
				}
			}
			if provider == "agent" && agentCommand == "" {
				if agentCommand = detectAgentCLI(); agentCommand == "" {
					uiLog.Warnf("no coding-agent CLI (claude/omp/codex) on PATH; semantic reranking off, structural search still works")
				}
			}

			if !asJSON {
				fmt.Fprintf(out, "Indexing %s ...\n", root)
			}
			langs, langsSet := parseLanguagesFlag(languagesValue)
			healed := false
			if !langsSet {
				if healedLangs, ok := shouldHealLanguages(root); ok {
					langs, langsSet, healed = healedLangs, true, true
				}
			}
			sum, err := RunInit(InitOptions{Root: root, AI: ai, AISet: aiSet, Tier: tier, EmbedModel: embedModel, AssistModel: assistModel, Provider: provider, AgentCommand: agentCommand, Integrations: integrations, IntegrationsSet: true, Languages: langs, LanguagesSet: langsSet})
			if err != nil {
				return err
			}
			// Run AI setup against the final saved models (resolved or preserved).
			if ai && provider != "agent" {
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
			if f, ok := out.(*os.File); ok && isTTY(f) {
				// Pull languages and the resolution split for the card; the index
				// just ran, so this open is a fast no-op refresh.
				var langs map[string]int
				resolved := 0
				if q, _, s, closer, e := openQuerier(cmd.Context(), false); e == nil {
					if st, e2 := q.Status(); e2 == nil {
						langs, resolved, sum.Edges = st.Counts.Langs, st.Counts.Resolved, st.Counts.Edges
					}
					_ = s
					_ = closer()
				}
				fmt.Fprintln(out, renderInitCard(filepath.Base(root), sum.Indexed, sum.Symbols, sum.Edges, resolved, langs, integrations, ai))
			} else {
				fmt.Fprintf(out, "Prowl Agent ready: %d files indexed (%d symbols, %d edges).\n", sum.Indexed, sum.Symbols, sum.Edges)
				fmt.Fprintln(out, "Query it from your shell, no server to run:")
				fmt.Fprintln(out, "  prowl-agent overview        a map of this project")
				fmt.Fprintln(out, "  prowl-agent find <name>     locate any symbol")
				fmt.Fprintln(out, "  prowl-agent search <text>   search by meaning or text")
				fmt.Fprintln(out, "  prowl-agent docs add <url>  index external documentation")
				fmt.Fprintf(out, "%d selected integration(s) configured; .prowl/ is gitignored.\n", len(integrations))
			}
			if healed {
				fmt.Fprintln(out, "Notice: .prowl/config.toml indexed only a minority of this repo, so indexing was reset to all detected languages (languages = auto). Run 'prowl-agent init --languages <list>' to keep a narrow set.")
			}
			for _, w := range unindexedLanguageWarnings(root) {
				fmt.Fprintf(out, "Warning: %s\n", w)
			}
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
	c.Flags().StringVar(&languagesValue, "languages", "", "comma-separated languages to index, or auto (default: keep existing config)")
	c.Flags().StringVar(&aiProvider, "ai-provider", "", "semantic-assist backend: ollama (local model) or agent (borrow a coding-agent CLI for reranking)")
	c.Flags().StringVar(&aiCommand, "ai-command", "", "completion command when --ai-provider=agent, e.g. \"claude -p --model haiku\"; default: autodetect a cheap tier")
	c.MarkFlagsMutuallyExclusive("with-ai", "no-ai")
	return c
}

// parseLanguagesFlag turns the --languages value into an explicit language list.
// An empty value leaves the existing config untouched; "auto" indexes everything.
func parseLanguagesFlag(value string) ([]string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	var langs []string
	for _, part := range strings.Split(value, ",") {
		if p := strings.TrimSpace(part); p != "" {
			langs = append(langs, p)
		}
	}
	if len(langs) == 0 {
		return nil, false
	}
	return langs, true
}

func printSetupPlan(out io.Writer, plan SetupPlan, asJSON, dryRun bool) error {
	if asJSON {
		return json.NewEncoder(out).Encode(map[string]any{"dry_run": dryRun, "plan": plan})
	}
	fmt.Fprintf(out, "Setup plan for %s\n", collapseHome(plan.Root))
	fmt.Fprintln(out, "  • create or refresh the local .prowl workspace and index")
	fmt.Fprintln(out, "  • preserve existing project configuration and rules")
	// Collapse the per-file skill actions (one per SKILL.md per agent) into a
	// single summary line so the plan stays scannable instead of a wall.
	var skillClients []string
	seenClient := map[string]bool{}
	for _, action := range plan.Actions {
		if action.Integration == "skill" {
			client := strings.TrimPrefix(strings.SplitN(action.Path, "/", 2)[0], ".")
			if client != "" && !seenClient[client] {
				seenClient[client] = true
				skillClients = append(skillClients, client)
			}
			continue
		}
		fmt.Fprintf(out, "  • %-12s %s\n", action.Integration, action.Path)
	}
	if len(skillClients) > 0 {
		fmt.Fprintf(out, "  • %-12s prowl skills for %s\n", "skills", strings.Join(skillClients, ", "))
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

// detectAgentCLI returns a headless completion command for the first installed
// coding-agent CLI, or "" when none is found. Reranking is a lightweight
// ordering task, not coding, so each command pins the agent's cheapest/fastest
// model tier -- prowl is a support tool and the spawn must stay cheap. The
// command is fully overridable via --ai-command / config for a different model.
func detectAgentCLI() string {
	for _, cand := range []struct{ bin, command string }{
		{"claude", "claude -p --model haiku"},
		{"omp", "omp -p --model haiku"},
		{"codex", "codex exec -m gpt-5-mini"},
	} {
		if _, err := exec.LookPath(cand.bin); err == nil {
			return cand.command
		}
	}
	return ""
}
