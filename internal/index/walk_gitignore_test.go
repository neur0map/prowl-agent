package index

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWalkNegationReinclude covers the monorepo pattern of ignoring a tree and
// re-including a subtree (`packages/*/*/` then `!packages/*/src/`), plus the
// dir-only trailing slash: a dir-only pattern must not hide same-depth files
// such as package.json. Without this, a pnpm/turbo workspace's source tree
// disappears from the index.
func TestWalkNegationReinclude(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "dist/\npackages/*/*/\n!packages/*/src/\n!packages/*/test/\n")
	// Kept: dir-only pattern does not hide the same-depth manifest, and the
	// re-included src/test subtrees are walked.
	write("packages/server/package.json", `{"name":"@x/server"}`)
	write("packages/server/src/index.ts", "x")
	write("packages/server/src/deep/util.ts", "x")
	write("packages/server/test/a.ts", "x")
	// Ignored: dist tree, and a non-re-included subtree under packages/*/*.
	write("packages/server/dist/index.js", "x")
	write("dist/bundle.js", "x")
	write("packages/server/build/out.js", "x")

	got, err := Walk(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	gotSet := map[string]bool{}
	for _, g := range got {
		gotSet[g] = true
	}
	wantPresent := []string{
		".gitignore",
		"packages/server/package.json",
		"packages/server/src/index.ts",
		"packages/server/src/deep/util.ts",
		"packages/server/test/a.ts",
	}
	for _, w := range wantPresent {
		if !gotSet[w] {
			t.Errorf("missing %q from walk %v", w, got)
		}
	}
	wantAbsent := []string{
		"packages/server/dist/index.js",
		"dist/bundle.js",
		"packages/server/build/out.js",
	}
	for _, w := range wantAbsent {
		if gotSet[w] {
			t.Errorf("unexpectedly walked ignored file %q", w)
		}
	}
}
