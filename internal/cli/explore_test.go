package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

// listTree returns every path under root, relative and sorted, so a test can
// assert the target repo is byte-for-byte untouched across a command run.
func listTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel != "." {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

func runExplore(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newExploreCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("explore %v: %v\n%s", args, err, buf.String())
	}
	return buf.String()
}

// TestExploreLeavesTargetUntouched proves explore never pollutes the repo it
// reviews. It indexes into an ephemeral scratch dir, so the target gains no
// .prowl/, .gitignore, or AGENTS.md and keeps its exact file set, on both the
// overview and the --question paths.
func TestExploreLeavesTargetUntouched(t *testing.T) {
	root := t.TempDir()
	src := "package widget\n\n// Battery reports the current charge percent.\nfunc Battery() int { return 42 }\n"
	if err := os.WriteFile(filepath.Join(root, "widget.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	before := listTree(t, root)

	if out := runExplore(t, root); out == "" {
		t.Fatal("explore overview produced no output")
	}
	if out := runExplore(t, root, "--question", "battery charge"); out == "" {
		t.Fatal("explore --question produced no output")
	}

	if after := listTree(t, root); !slices.Equal(before, after) {
		t.Fatalf("explore changed the target tree:\nbefore=%v\nafter=%v", before, after)
	}
	for _, polluted := range []string{".prowl", ".gitignore", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(root, polluted)); !os.IsNotExist(err) {
			t.Fatalf("explore created %s in the target (stat err=%v)", polluted, err)
		}
	}
}
