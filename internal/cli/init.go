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
	"time"

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
	Root        string
	Tier        string
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
	// EmbedProgress, when set, receives semantic-index build progress. The first
	// build on a large repo is thousands of model round trips; without a signal it
	// is indistinguishable from a hang.
	EmbedProgress func(embedded, remaining int)
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

	// Semantic assist is always on; init never disables it. A fresh project and
	// a re-init both land on enabled=true, healing any legacy config written
	// back when AI could be skipped.
	cfg.AI.Enabled = true

	tier := firstNonEmpty(opt.Tier, g.Tier, config.DefaultTier)
	switch {
	case opt.Tier != "":
		p := config.PresetByName(opt.Tier)
		cfg.AI.AssistModel = p.AssistModel
	case !existed:
		p := config.PresetByName(tier)
		cfg.AI.AssistModel = firstNonEmpty(g.AssistModel, p.AssistModel)
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
	if opt.LanguagesSet {
		cfg.Languages = opt.Languages
	}

	if err := config.Save(ws.Path, cfg); err != nil {
		return index.Summary{}, err
	}
	// Remember tier/models binary-wide so future inits inherit them, but only on
	// a brand-new project or an explicit tier choice: a plain re-index of an
	// existing project must not silently change the global default.
	if opt.Tier != "" || !existed {
		_ = config.SaveGlobal(config.GlobalConfig{
			AIEnabled:   true,
			Tier:        tier,
			AssistModel: cfg.AI.AssistModel,
		})
	}

	// Write starter rules only when absent, so a re-init keeps user-edited rules.
	if _, statErr := os.Stat(filepath.Join(ws.Path, "rules.toml")); os.IsNotExist(statErr) {
		if err := config.SaveRules(ws.Path, config.DefaultRules()); err != nil {
			return index.Summary{}, err
		}
	}
	// Open with AI so init builds the semantic index it reports as ready. Without
	// an inferencer, init printed "semantic search ready" while embedding nothing.
	project, err := application.OpenProject(context.Background(), root, application.Options{
		EnableAI: cfg.AI.Enabled, InferencerProvider: maybeInferencer,
	})
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

	// init is the explicit setup step, so it drains the embedding backlog rather
	// than leaving the semantic index it advertises half-built.
	var embedProgress func(index.VectorPass)
	if opt.EmbedProgress != nil {
		embedProgress = func(pass index.VectorPass) { opt.EmbedProgress(pass.Embedded, pass.Remaining) }
	}
	if _, embedErr := project.BuildSemanticIndex(context.Background(), embedProgress); embedErr != nil {
		return sum, embedErr
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
	if err := workspace.Register(root, true); err != nil {
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
	var yes, noInput, reconfigure, dryRun, asJSON, remove bool
	var tier, integrationValue, languagesValue, aiProvider, aiCommand string
	c := &cobra.Command{
		Use:   "init",
		Short: "Plan, preview, and set up Prowl in the current folder",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _ := os.Getwd()
			out := cmd.OutOrStdout()
			nonInteractive := yes || noInput || dryRun || asJSON

			detected := DetectIntegrations(root)
			// Fold in the harnesses the user actually runs (omp, claude) even when
			// this repo has no config dir for them yet, so `auto` also installs
			// their native integration -- the harness's own skills (its "when to
			// reach for prowl" signal) plus a native MCP entry -- not only the
			// client-agnostic AGENTS.md/.mcp.json baseline. This is what makes
			// every agent, not just MCP-aware ones, know prowl out of the box.
			detected = append(detected, DetectInstalledHarnesses()...)
			var integrations []string
			var err error
			if cmd.Flags().Changed("integrations") || nonInteractive {
				integrations, err = ParseIntegrationSelection(integrationValue, detected)
				if err != nil {
					return err
				}
			} else {
				// Pre-select the same universal baseline `auto` installs (any
				// detected clients plus the client-agnostic AGENTS.md guidance and
				// `.mcp.json` MCP registration), so accepting the default wires
				// Prowl up instead of installing nothing on a repo with no detected
				// client -- the trap that left indexed repos with no signal that
				// Prowl exists. The user can still deselect any of them.
				integrations, err = ParseIntegrationSelection("auto", detected)
				if err != nil {
					return err
				}
				options := make([]huh.Option[string], 0, len(allIntegrations))
				for _, name := range allIntegrations {
					options = append(options, huh.NewOption(name, name).Selected(containsString(integrations, name)))
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

			// AI-assisted semantic search is always on and cannot be skipped at
			// init. The backend and tier below are still user-choosable; the
			// runtime resolver degrades gracefully when no local model exists.

			// Resolve the semantic-assist backend. An explicit --ai-provider (or a
			// saved one) wins; otherwise prefer a local model (Ollama: embeddings +
			// reranking) when installed, else borrow a coding-agent CLI (reranking
			// only) so semantic assist is meaningful even without a local model.
			provider, agentCommand := aiProvider, aiCommand
			var assistModel string
			if provider == "" {
				if pc, e := config.Load(projDir); e == nil {
					provider = pc.AI.Provider
					if agentCommand == "" {
						agentCommand = pc.AI.AgentCommand
					}
				}
			}
			if provider == "" {
				detected := detectAgentCLI()
				_, ollamaErr := exec.LookPath("ollama")
				ollamaInstalled := ollamaErr == nil
				interactive := !nonInteractive && !yes && (reconfigure || !remembered)
				switch {
				case interactive && detected != "":
					// Both viable: let the user pick the optional upgrade. Semantic
					// search is already on via the built-in embedder.
					provider = selectBackend(detected, ollamaInstalled)
					if provider == "agent" {
						agentCommand = detected
					}
				case ollamaInstalled:
					provider = "ollama"
				case detected != "":
					provider, agentCommand = "agent", detected
					uiLog.Infof("semantic search is on (built-in embedder); %q adds rewrite+rerank (cheap tier). Override with --ai-command", agentCommand)
				default:
					uiLog.Infof("semantic search is on (built-in embedder); install Ollama or a coding agent to add query rewrite and rerank")
				}
			}
			if provider == "agent" && agentCommand == "" {
				if agentCommand = detectAgentCLI(); agentCommand == "" {
					uiLog.Warnf("--ai-provider agent but no coding-agent CLI (claude/omp/codex) on PATH; rewrite+rerank off, semantic search still on via the built-in embedder")
				}
			}
			if provider == "ollama" && (reconfigure || !remembered || tier != "") {
				if tier == "" {
					tier = firstNonEmpty(g.Tier, config.DefaultTier)
					if !nonInteractive && (reconfigure || !remembered) {
						tier = selectTier()
					}
				}
				p := config.PresetByName(tier)
				oll := assist.NewOllama("", p.AssistModel)
				assistModel = resolveAssistModel(cmd.Context(), oll, p)
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
			var embedProgress func(int, int)
			if !asJSON {
				lastReport := time.Time{}
				embedProgress = func(embedded, remaining int) {
					if remaining > 0 && time.Since(lastReport) < time.Second {
						return
					}
					lastReport = time.Now()
					fmt.Fprintf(out, "\rBuilding semantic index: %d embedded, %d to go ...", embedded, remaining)
					if remaining == 0 {
						fmt.Fprintln(out)
					}
				}
			}
			sum, err := RunInit(InitOptions{Root: root, Tier: tier, AssistModel: assistModel, Provider: provider, AgentCommand: agentCommand, Integrations: integrations, IntegrationsSet: true, Languages: langs, LanguagesSet: langsSet, EmbedProgress: embedProgress})
			if err != nil {
				return err
			}
			// Run AI setup against the final saved models (resolved or preserved).
			if provider == "ollama" {
				final, _ := config.Load(projDir)
				if tier == "" {
					tier = firstNonEmpty(g.Tier, config.DefaultTier)
				}
				aiOut := io.Writer(out)
				if asJSON {
					aiOut = io.Discard
				}
				setupAI(cmd.Context(), aiOut, config.ModelPreset{Name: tier, AssistModel: final.AI.AssistModel}, !nonInteractive)
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
				fmt.Fprintln(out, renderInitCard(filepath.Base(root), sum.Indexed, sum.Symbols, sum.Edges, resolved, langs, integrations, true))
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
