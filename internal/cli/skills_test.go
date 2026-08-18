package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/prowl-agent/prowl-agent/internal/setup"
)

// skillsOpts builds a user-install target confined to temporary directories so a
// runSkills exercise never touches the real user's config roots. Clients are
// explicit (production supplies DetectInstalledHarnesses) so planning is
// deterministic regardless of what harnesses the test host happens to have.
func skillsOpts(t *testing.T, clients ...string) setup.UserInstallOptions {
	t.Helper()
	if len(clients) == 0 {
		clients = []string{"claude"}
	}
	return setup.UserInstallOptions{
		Home:     t.TempDir(),
		StateDir: t.TempDir(),
		Version:  "9.9.9",
		Clients:  clients,
	}
}

// manifestPresent reports whether an apply committed the ownership manifest. Its
// presence is the single reliable signal that a write actually happened: no
// apply, no manifest.
func manifestPresent(opts setup.UserInstallOptions) bool {
	_, err := os.Stat(filepath.Join(opts.StateDir, "prowl-agent", "agent-assets.json"))
	return err == nil
}

// TestSkillsNonInteractivePreviewWritesNothing proves a non-interactive run is a
// pure preview: it renders the plan and commits nothing, so an agent shelling
// out to `prowl-agent skills` in a pipe can never mutate the user's config.
func TestSkillsNonInteractivePreviewWritesNothing(t *testing.T) {
	opts := skillsOpts(t)
	var out bytes.Buffer
	if err := runSkills(opts, strings.NewReader(""), &out, false); err != nil {
		t.Fatalf("runSkills: %v", err)
	}
	if !strings.Contains(strings.ToLower(out.String()), "nothing was written") {
		t.Errorf("non-interactive run did not report a no-write preview:\n%s", out.String())
	}
	if manifestPresent(opts) {
		t.Error("non-interactive run committed the ownership manifest")
	}
	if _, err := os.Stat(filepath.Join(opts.Home, ".claude", "skills", "prowl", "commands", "search.md")); err == nil {
		t.Error("non-interactive run installed an asset")
	}
}

// TestSkillsDefaultNoKeepsFilesystemUntouched proves that in interactive mode the
// default answer is No: both an empty line and an explicit "n" decline, and
// neither writes anything.
func TestSkillsDefaultNoKeepsFilesystemUntouched(t *testing.T) {
	for _, answer := range []string{"", "\n", "n\n", "N\n", "no\n"} {
		opts := skillsOpts(t)
		var out bytes.Buffer
		if err := runSkills(opts, strings.NewReader(answer), &out, true); err != nil {
			t.Fatalf("runSkills(%q): %v", answer, err)
		}
		if manifestPresent(opts) {
			t.Errorf("answer %q applied changes despite a No default", answer)
		}
		if _, err := os.Stat(filepath.Join(opts.Home, ".claude", "skills", "prowl")); err == nil {
			t.Errorf("answer %q installed assets despite a No default", answer)
		}
	}
}

// TestSkillsYesAppliesPlanOnce proves an explicit "y" (and "yes") applies the
// reviewed plan: assets land under the client root and the ownership manifest is
// committed exactly once.
func TestSkillsYesAppliesPlanOnce(t *testing.T) {
	for _, answer := range []string{"y\n", "yes\n", "Y\n", "YES\n"} {
		opts := skillsOpts(t)
		var out bytes.Buffer
		if err := runSkills(opts, strings.NewReader(answer), &out, true); err != nil {
			t.Fatalf("runSkills(%q): %v", answer, err)
		}
		if !manifestPresent(opts) {
			t.Errorf("answer %q did not commit the ownership manifest", answer)
		}
		asset := filepath.Join(opts.Home, ".claude", "skills", "prowl", "commands", "search.md")
		if _, err := os.Stat(asset); err != nil {
			t.Errorf("answer %q did not install %s: %v", answer, asset, err)
		}
		if !strings.Contains(strings.ToLower(out.String()), "installed") {
			t.Errorf("answer %q did not report the apply:\n%s", answer, out.String())
		}
	}
}

// TestSkillsConflictsPreservedAndPrinted proves a destination Prowl does not own
// is surfaced as a conflict, printed clearly, and left byte-for-byte untouched by
// an approved apply, while the other assets still install.
func TestSkillsConflictsPreservedAndPrinted(t *testing.T) {
	opts := skillsOpts(t)
	foreign := filepath.Join(opts.Home, ".claude", "skills", "prowl", "commands", "search.md")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	const keep = "someone else's command\n"
	if err := os.WriteFile(foreign, []byte(keep), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runSkills(opts, strings.NewReader("y\n"), &out, true); err != nil {
		t.Fatalf("runSkills: %v", err)
	}

	rendered := out.String()
	if !strings.Contains(strings.ToLower(rendered), "conflict") {
		t.Errorf("conflict was not printed:\n%s", rendered)
	}
	if !strings.Contains(rendered, "commands/search.md") {
		t.Errorf("conflict destination was not named:\n%s", rendered)
	}
	if got, _ := os.ReadFile(foreign); string(got) != keep {
		t.Errorf("apply overwrote the unowned file: %q", got)
	}
	// A non-conflicting asset still installs alongside the preserved conflict.
	if _, err := os.Stat(filepath.Join(opts.Home, ".claude", "skills", "prowl", "hooks", "hooks.json")); err != nil {
		t.Errorf("apply skipped a clean asset because of an unrelated conflict: %v", err)
	}
}

// TestSkillsOutputNamesRestartReload proves a successful install tells the user to
// reload each detected client, so the freshly installed skills actually take
// effect.
func TestSkillsOutputNamesRestartReload(t *testing.T) {
	opts := skillsOpts(t, "claude", "omp")
	var out bytes.Buffer
	if err := runSkills(opts, strings.NewReader("y\n"), &out, true); err != nil {
		t.Fatalf("runSkills: %v", err)
	}
	lower := strings.ToLower(out.String())
	for _, want := range []string{"restart", "claude", "reload", "omp"} {
		if !strings.Contains(lower, want) {
			t.Errorf("apply output did not name the %q step:\n%s", want, out.String())
		}
	}
}

// TestSkillsCommandHasNoArgsFlagsOrSubcommands proves the public command is a
// single, argument-free installer: no positional arguments, no subcommand, and
// no confirmation-bypass flag that would let a caller skip the review prompt.
func TestSkillsCommandHasNoArgsFlagsOrSubcommands(t *testing.T) {
	cmd := newSkillsCmd("test")
	if cmd.Args == nil {
		t.Fatal("skills command accepts arbitrary positional arguments")
	}
	if err := cmd.Args(cmd, []string{"extra"}); err == nil {
		t.Error("skills command accepted a positional argument")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("skills command rejected a bare invocation: %v", err)
	}
	if subs := cmd.Commands(); len(subs) != 0 {
		t.Errorf("skills command exposes subcommands: %v", subs)
	}
	var flags []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { flags = append(flags, f.Name) })
	if len(flags) != 0 {
		t.Errorf("skills command defines local flags (possible confirmation bypass): %v", flags)
	}
}

// TestSearchAdvisoryClassification pins the conservative advisory classifier: only
// repository-wide native searches and shell rg/grep/find earn an advisory, while
// bounded, named, prowl-agent, unknown, and malformed inputs stay silent. False
// negatives are acceptable; the hook is advisory, never policy.
func TestSearchAdvisoryClassification(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		advise  bool
	}{
		{"repo-wide grep", `{"hook_event_name":"PreToolUse","tool_name":"Grep","tool_input":{"pattern":"TODO"}}`, true},
		{"repo-wide grep dot path", `{"tool_name":"Grep","tool_input":{"pattern":"TODO","path":"."}}`, true},
		{"repo-wide glob", `{"tool_name":"Glob","tool_input":{"pattern":"**/*.go"}}`, true},
		{"shell rg", `{"tool_name":"Bash","tool_input":{"command":"rg TODO"}}`, true},
		{"shell grep recursive", `{"tool_name":"Bash","tool_input":{"command":"grep -rn TODO ."}}`, true},
		{"shell find", `{"tool_name":"Bash","tool_input":{"command":"find . -name '*.go'"}}`, true},
		{"shell grep in pipeline", `{"tool_name":"Bash","tool_input":{"command":"cat notes | grep TODO"}}`, true},
		{"bounded grep", `{"tool_name":"Grep","tool_input":{"pattern":"TODO","path":"internal/cli"}}`, false},
		{"bounded glob", `{"tool_name":"Glob","tool_input":{"pattern":"*.go","path":"internal/cli"}}`, false},
		{"prowl-agent bash", `{"tool_name":"Bash","tool_input":{"command":"prowl-agent search TODO"}}`, false},
		{"named read", `{"tool_name":"Read","tool_input":{"file_path":"/repo/main.go"}}`, false},
		{"unknown tool", `{"tool_name":"Edit","tool_input":{"file_path":"/repo/main.go"}}`, false},
		{"non-search bash", `{"tool_name":"Bash","tool_input":{"command":"go test ./..."}}`, false},
		{"malformed json", `{not valid`, false},
		{"empty input", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := runSearchAdvisory(strings.NewReader(tc.payload), &out); err != nil {
				t.Fatalf("runSearchAdvisory returned error (must fail open): %v", err)
			}
			emitted := strings.TrimSpace(out.String()) != ""
			if emitted != tc.advise {
				t.Errorf("advise=%v, want %v (output=%q)", emitted, tc.advise, out.String())
			}
		})
	}
}

// TestSearchAdvisoryOutputShape proves an emitted advisory is exactly Claude's
// additionalContext response for PreToolUse and carries no permission decision,
// so the hook can inform but never approve, ask, defer, or deny.
func TestSearchAdvisoryOutputShape(t *testing.T) {
	var out bytes.Buffer
	if err := runSearchAdvisory(strings.NewReader(`{"tool_name":"Grep","tool_input":{"pattern":"TODO"}}`), &out); err != nil {
		t.Fatalf("runSearchAdvisory: %v", err)
	}
	if strings.Contains(out.String(), "permissionDecision") {
		t.Fatalf("advisory carried a permission decision:\n%s", out.String())
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("advisory is not valid JSON: %v\n%s", err, out.String())
	}
	raw, ok := payload["hookSpecificOutput"]
	if !ok {
		t.Fatalf("advisory missing hookSpecificOutput: %s", out.String())
	}
	var hook struct {
		HookEventName      string `json:"hookEventName"`
		AdditionalContext  string `json:"additionalContext"`
		PermissionDecision string `json:"permissionDecision"`
	}
	if err := json.Unmarshal(raw, &hook); err != nil {
		t.Fatal(err)
	}
	if hook.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q, want PreToolUse", hook.HookEventName)
	}
	if strings.TrimSpace(hook.AdditionalContext) == "" {
		t.Error("advisory carried an empty additionalContext")
	}
	if hook.PermissionDecision != "" {
		t.Errorf("advisory carried a permission decision: %q", hook.PermissionDecision)
	}
}

// TestSearchAdvisoryNeverEchoesInput proves the advisory is constant: it never
// copies the caller's tool input (a pattern, path, or command) into its output,
// so a hook can never be used to reflect attacker-controlled text back to Claude.
func TestSearchAdvisoryNeverEchoesInput(t *testing.T) {
	const secret = "ZZ_SUPER_SECRET_PATTERN_ZZ"
	inputs := []string{
		`{"tool_name":"Grep","tool_input":{"pattern":"` + secret + `"}}`,
		`{"tool_name":"Glob","tool_input":{"pattern":"**/` + secret + `"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"grep -r ` + secret + ` ."}}`,
	}
	for _, in := range inputs {
		var out bytes.Buffer
		if err := runSearchAdvisory(strings.NewReader(in), &out); err != nil {
			t.Fatalf("runSearchAdvisory: %v", err)
		}
		if strings.TrimSpace(out.String()) == "" {
			t.Fatalf("expected an advisory for %q", in)
		}
		if strings.Contains(out.String(), secret) {
			t.Errorf("advisory echoed caller input:\n%s", out.String())
		}
	}
}
