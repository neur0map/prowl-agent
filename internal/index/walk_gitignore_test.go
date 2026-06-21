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

// TestWalkNestedGitignore checks that a nested .gitignore is honored, scoped to
// its own directory: a per-package `dist/` ignore excludes that package's build
// output without any root rule, while the same path in a sibling package (with
// no nested ignore) is still indexed, and the root rule still applies in the
// subtree. A nested file-glob negation re-includes within the subtree.
func TestWalkNestedGitignore(t *testing.T) {
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
	write(".gitignore", "*.log\n")                              // root: ignore logs everywhere
	write("packages/a/.gitignore", "dist/\n*.tmp\n!keep.tmp\n") // nested, scoped to packages/a
	write("packages/a/src/index.ts", "x")
	write("packages/a/dist/bundle.js", "x") // ignored by nested dist/
	write("packages/a/scratch.tmp", "x")    // ignored by nested *.tmp
	write("packages/a/keep.tmp", "x")       // re-included by nested !keep.tmp
	write("packages/a/debug.log", "x")      // ignored by the root rule, in the subtree
	write("packages/b/dist/bundle.js", "x") // no nested gitignore in b -> indexed

	got, err := Walk(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	in := map[string]bool{}
	for _, g := range got {
		in[g] = true
	}
	for _, want := range []string{"packages/a/src/index.ts", "packages/a/keep.tmp", "packages/b/dist/bundle.js"} {
		if !in[want] {
			t.Errorf("missing %q from walk %v", want, got)
		}
	}
	for _, gone := range []string{"packages/a/dist/bundle.js", "packages/a/scratch.tmp", "packages/a/debug.log"} {
		if in[gone] {
			t.Errorf("unexpectedly walked ignored file %q", gone)
		}
	}
}
