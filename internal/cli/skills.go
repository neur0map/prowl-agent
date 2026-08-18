// Package cli: the user-facing agent-skill installer (`skills`) and the hidden
// Claude PreToolUse advisory hook (`_search-advisory`).
//
// `skills` is a thin, testable presenter over Task 4's ownership-safe user
// installer (internal/setup): it detects the harnesses actually installed on the
// machine, builds a deterministic plan, renders every action and conflict, and
// -- only in an interactive terminal, only on explicit y/yes -- applies it once.
// It reuses PlanUserSkills/ApplyUserSkills/VerifyUserSkills wholesale and never
// re-implements ownership checks or filesystem mutation.
//
// `_search-advisory` is the command Claude runs from the installed hooks.json on
// every Grep/Glob/Bash. It reads one PreToolUse JSON object, classifies only
// repository-wide searches, and either stays silent or emits Claude's
// additionalContext response nudging the agent toward Prowl's indexed search. It
// fails open: malformed, unknown, bounded, or prowl-agent input produces no
// output and exits 0. It never emits a permission decision and never echoes the
// caller's tool input.
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/setup"
)

// newSkillsCmd installs Prowl's agent-native skills under the user's own Claude
// and OMP configuration roots. It takes no positional arguments, has no
// subcommand, and deliberately exposes no confirmation-bypass flag: the review
// prompt is the only path to a write, and a non-interactive invocation is always
// a preview. version stamps the version-templated assets (only Claude's plugin
// manifest carries it).
func newSkillsCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "skills",
		Short: "Install Prowl's agent-native skills into your Claude and OMP config",
		Long: "Install Prowl's release-matched agent skills into your own Claude and OMP\n" +
			"configuration roots (~/.claude/skills/prowl and ~/.omp/agent). The command\n" +
			"shows a full preview of every file it would write and every destination it\n" +
			"refuses to touch, then asks once before changing anything. Piped or\n" +
			"non-interactive runs are always a preview and never write.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			opts := setup.UserInstallOptions{
				Home:    home,
				Version: version,
				Clients: setup.DetectInstalledHarnesses(),
			}
			return runSkills(opts, cmd.InOrStdin(), cmd.OutOrStdout(), interactiveTerminal())
		},
	}
}

// runSkills is the presenter seam: injected input, output, and interactivity make
// the preview, TTY gate, prompt, single apply, and restart guidance observable in
// a unit test without a real terminal. It plans, renders, and -- only when
// interactive and explicitly approved -- applies exactly once.
func runSkills(opts setup.UserInstallOptions, in io.Reader, out io.Writer, interactive bool) error {
	if len(opts.Clients) == 0 {
		fmt.Fprintln(out, "No supported agent detected (looked for Claude and OMP); nothing to install.")
		return nil
	}

	plan, err := setup.PlanUserSkills(opts)
	if err != nil {
		return err
	}
	renderSkillsPlan(out, plan)

	if !planHasWrites(plan) {
		fmt.Fprintln(out, "\nEverything is already up to date; nothing to apply.")
		return nil
	}
	if !interactive {
		fmt.Fprintln(out, "\nPreview only: no interactive terminal attached, so nothing was written. Re-run in a terminal to apply.")
		return nil
	}

	fmt.Fprint(out, "\nApply these changes? [y/N] ")
	if !confirmYes(in) {
		fmt.Fprintln(out, "No changes applied.")
		return nil
	}

	result, err := setup.ApplyUserSkills(opts, plan, true)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nInstalled %d change(s) for version %s.\n", countWrites(result.Actions), result.Version)
	if health, err := setup.VerifyUserSkills(opts); err == nil {
		fmt.Fprintf(out, "Verified %d asset(s) as current.\n", countCurrent(health))
	}
	renderRestart(out, opts.Clients)
	return nil
}

// renderSkillsPlan prints the reviewable plan: every write it would make and
// every destination it refuses to touch, with the reason. It carries no file
// bodies and no absolute paths -- the plan is already home-relative.
func renderSkillsPlan(out io.Writer, plan setup.UserPlan) {
	fmt.Fprintf(out, "Prowl agent-skills install plan (version %s)\n", plan.Version)

	writes := writeActions(plan)
	if len(writes) == 0 {
		fmt.Fprintln(out, "  (no changes)")
	} else {
		fmt.Fprintln(out, "\nChanges:")
		for _, action := range writes {
			fmt.Fprintf(out, "  %-9s %-6s %s\n", action.Kind, action.Client, action.Destination)
		}
	}

	if len(plan.Conflicts) > 0 {
		fmt.Fprintln(out, "\nConflicts (left unchanged):")
		for _, conflict := range plan.Conflicts {
			fmt.Fprintf(out, "  %-6s %s -- %s\n", conflict.Client, conflict.Destination, conflict.Reason)
		}
	}
}

// renderRestart names the reload each detected client needs so the freshly
// installed skills take effect. The clients come straight from the plan's
// options, so only what was targeted is mentioned.
func renderRestart(out io.Writer, clients []string) {
	fmt.Fprintln(out, "\nNext steps:")
	for _, client := range clients {
		switch client {
		case setup.IntegrationClaude:
			fmt.Fprintln(out, "  - Restart Claude Code so it loads the prowl skills plugin.")
		case setup.IntegrationOMP:
			fmt.Fprintln(out, "  - Reload OMP so it picks up the prowl agent skills.")
		}
	}
}

// writeActions returns the plan's mutating actions (install/update/remove),
// dropping the unchanged entries that carry no news for a preview.
func writeActions(plan setup.UserPlan) []setup.UserAction {
	var out []setup.UserAction
	for _, action := range plan.Actions {
		if action.Kind != setup.UserActionUnchanged {
			out = append(out, action)
		}
	}
	return out
}

func planHasWrites(plan setup.UserPlan) bool {
	return len(writeActions(plan)) > 0
}

func countWrites(actions []setup.UserAction) int {
	n := 0
	for _, action := range actions {
		if action.Kind != setup.UserActionUnchanged {
			n++
		}
	}
	return n
}

func countCurrent(health setup.UserHealth) int {
	n := 0
	for _, asset := range health.Assets {
		if asset.State == setup.UserAssetCurrent {
			n++
		}
	}
	return n
}

// confirmYes reads one line and approves only an explicit y/yes (case- and
// whitespace-insensitive). Everything else -- an empty line, EOF, "n", or any
// other text -- keeps the safe default of No.
func confirmYes(in io.Reader) bool {
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// interactiveTerminal reports whether both stdin and stdout are terminals. A
// write happens only when a human can actually review the prompt on the same
// terminal that will read the answer, so a pipe on either side stays a preview.
func interactiveTerminal() bool {
	return isCharDevice(os.Stdin) && isCharDevice(os.Stdout)
}

func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// searchAdvisoryContext is the constant guidance the hook injects for a
// repository-wide search. It is fixed text: the hook never reflects the caller's
// tool input, so it can neither leak nor echo attacker-controlled strings.
const searchAdvisoryContext = "Prowl indexes this repository for token-lean, cited code search. " +
	"Before a repository-wide scan, consider `prowl-agent search`, `prowl-agent find`, " +
	"`prowl-agent def`, `prowl-agent references`, or `prowl-agent outline`: they return " +
	"ranked, cited results without reading every file. This is advisory only."

// maxAdvisoryInput bounds the PreToolUse payload the hook will read so a runaway
// stdin can never exhaust memory. A real hook payload is a few kilobytes.
const maxAdvisoryInput = 1 << 20

// advisoryResponse is Claude's PreToolUse hook output shape for adding context.
// It intentionally has no permission-decision field: the hook informs, it never
// approves, asks, defers, or denies.
type advisoryResponse struct {
	HookSpecificOutput advisoryHook `json:"hookSpecificOutput"`
}

type advisoryHook struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// newSearchAdvisoryCmd is the hidden Claude hook: it reads one PreToolUse JSON
// object from stdin and, only for a repository-wide search, prints Claude's
// additionalContext advisory. It is not a human command and never appears in
// help.
func newSearchAdvisoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_search-advisory",
		Short:  "Claude PreToolUse hook that nudges broad searches toward Prowl (internal)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSearchAdvisory(cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

// runSearchAdvisory reads one PreToolUse object, classifies it, and emits either
// nothing or the constant advisory. It always fails open: any read, parse, or
// classification miss produces no output and a zero exit, because the hook is an
// advisory and must never block or perturb the agent.
func runSearchAdvisory(in io.Reader, out io.Writer) error {
	payload, err := io.ReadAll(io.LimitReader(in, maxAdvisoryInput))
	if err != nil {
		return nil
	}
	if !advisorySuggests(payload) {
		return nil
	}
	data, err := json.Marshal(advisoryResponse{
		HookSpecificOutput: advisoryHook{
			HookEventName:     "PreToolUse",
			AdditionalContext: searchAdvisoryContext,
		},
	})
	if err != nil {
		return nil
	}
	fmt.Fprintln(out, string(data))
	return nil
}

// advisorySuggests is the whole classifier: it flags only a repository-wide
// Grep/Glob and a shell rg/grep/find, and nothing else. It is deliberately
// conservative -- a false negative just means no nudge, while a false positive
// would train the agent to ignore the hook.
func advisorySuggests(payload []byte) bool {
	var req struct {
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return false
	}
	switch req.ToolName {
	case "Grep", "Glob":
		return grepGlobIsRepoWide(req.ToolInput)
	case "Bash":
		return bashRunsBroadSearch(req.ToolInput)
	default:
		return false
	}
}

// grepGlobIsRepoWide reports whether a native Grep/Glob has no narrowing path: an
// absent, empty, "." , "./", or "/" path scans the whole repository. Any other
// path is a bounded search and earns no advisory.
func grepGlobIsRepoWide(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return false
	}
	switch strings.TrimSpace(in.Path) {
	case "", ".", "./", "/":
		return true
	default:
		return false
	}
}

// bashRunsBroadSearch reports whether a Bash command runs rg/grep/find as a
// command. A command that invokes prowl-agent is exactly the behavior the hook
// wants, so it is never flagged.
func bashRunsBroadSearch(raw json.RawMessage) bool {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return false
	}
	command := in.Command
	if command == "" || strings.Contains(command, "prowl-agent") {
		return false
	}
	for _, segment := range shellSegments.Split(command, -1) {
		if isSearchBinary(firstCommandToken(segment)) {
			return true
		}
	}
	return false
}

// shellSegments splits a command at pipeline and sequence separators so grep at
// the head of any stage (`cat x | grep y`) is caught, without inspecting mere
// arguments.
var shellSegments = regexp.MustCompile(`\|\||&&|[|;\n]`)

// envAssignment matches a leading VAR=value prefix a segment may carry before its
// actual command word.
var envAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// firstCommandToken returns the basename of a segment's command word, skipping
// leading environment assignments.
func firstCommandToken(segment string) string {
	for _, field := range strings.Fields(segment) {
		if envAssignment.MatchString(field) {
			continue
		}
		return path.Base(field)
	}
	return ""
}

func isSearchBinary(name string) bool {
	switch name {
	case "rg", "grep", "egrep", "fgrep", "find":
		return true
	default:
		return false
	}
}
