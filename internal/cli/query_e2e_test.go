package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestQueryCLIE2E builds the real binary, inits a fixture, and drives the
// read-only query subcommands as plain shell invocations: the CLI-first path an
// agent uses with no MCP server and no `serve`. It asserts cited TOON output by
// default and JSON under --json.
func TestQueryCLIE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process e2e in -short mode")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "prowl-agent")
	build := exec.Command("go", "build", "-tags", "sqlite_fts5", "-o", bin, "./cmd/prowl-agent")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}

	root := t.TempDir()
	copyDir(t, filepath.Join("..", "..", "testdata", "sample-config"), root)
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	if _, err := RunInit(InitOptions{Root: root}); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	// Default output is TOON: a tabular header plus a cited row, served straight
	// from the shell with no server running.
	if out := run("find", "M.apply"); !strings.Contains(out, "nvim/lua/opts.lua") {
		t.Fatalf("find (toon): %s", out)
	}
	if out := run("overview"); !strings.Contains(out, "clusters") || !strings.Contains(out, "entrypoints") {
		t.Fatalf("overview: %s", out)
	}
	if out := run("impact", "hypr/colors.conf"); !strings.Contains(out, "hypr/hyprland.conf") {
		t.Fatalf("impact: %s", out)
	}
	if out := run("callers", "hypr/colors.conf"); !strings.Contains(out, "hypr/hyprland.conf") {
		t.Fatalf("callers: %s", out)
	}

	// --json switches format and honors the json field tags.
	if out := run("find", "M.apply", "--json"); !strings.Contains(out, `"file":"nvim/lua/opts.lua"`) {
		t.Fatalf("find --json: %s", out)
	}

	// For the same answer, TOON is strictly leaner than JSON.
	toon := run("overview")
	js := run("overview", "--json")
	if len(toon) >= len(js) {
		t.Fatalf("expected TOON overview (%d bytes) leaner than JSON (%d bytes)", len(toon), len(js))
	}
}
