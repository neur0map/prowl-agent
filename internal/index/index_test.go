package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestWalkIgnores(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write(".gitignore", "ignored/\n*.log\n")
	write("a.lua", "x")
	write("ignored/secret.lua", "x")
	write("debug.log", "x")
	write(".prowl/index.db", "x")
	write("sub/b.sh", "x")

	got, err := Walk(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"a.lua": true, "sub/b.sh": true, ".gitignore": true}
	if len(got) != len(want) {
		t.Fatalf("walk = %v, want keys %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Fatalf("unexpected walked file %q in %v", g, got)
		}
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestIndexFixture(t *testing.T) {
	s := openStore(t)
	root := filepath.Join("..", "..", "testdata", "sample-config")
	sum, err := Index(s, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Indexed != 11 || sum.Parsed != 11 || sum.Skipped != 0 {
		t.Fatalf("summary=%+v want Indexed=11 Parsed=11 Skipped=0", sum)
	}

	// Connectivity from resolution.
	mustResolve := func(rel, kind string) {
		id, err := s.FileID(rel)
		if err != nil {
			t.Fatalf("file %s: %v", rel, err)
		}
		in, _ := s.IncomingEdges("file", id, kind)
		if len(in) == 0 {
			t.Fatalf("%s has no incoming %s edges", rel, kind)
		}
	}
	mustResolve("hypr/colors.conf", "includes")        // sourced by hyprland.conf
	mustResolve("nvim/lua/opts.lua", "includes")       // require("opts")
	mustResolve("hypr/scripts/screenshot.sh", "binds") // bind exec script
	mustResolve("waybar/colors.css", "includes")       // @import
	mustResolve("scripts/power.sh", "references")      // waybar on-click

	// 'kitty' bind is an external bare command -> dangling.
	dang, _ := s.UnresolvedEdges("binds")
	foundKitty := false
	for _, e := range dang {
		if e.Raw == "kitty" {
			foundKitty = true
		}
	}
	if !foundKitty {
		t.Fatalf("expected dangling bind to 'kitty', got %+v", dang)
	}

	// Re-indexing unchanged content reparses nothing.
	sum2, err := Index(s, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum2.Parsed != 0 || sum2.Skipped != 11 {
		t.Fatalf("reindex summary=%+v want Parsed=0 Skipped=11", sum2)
	}
}

func TestIndexIncremental(t *testing.T) {
	s := openStore(t)
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.lua", "function one() end\n")
	write("b.lua", "function two() end\n")

	if sum, err := Index(s, root, nil); err != nil || sum.Parsed != 2 {
		t.Fatalf("initial sum=%+v err=%v", sum, err)
	}
	// No changes -> nothing reparsed.
	if sum, err := Index(s, root, nil); err != nil || sum.Parsed != 0 || sum.Skipped != 2 {
		t.Fatalf("noop sum=%+v err=%v", sum, err)
	}
	// Change one file -> exactly one reparse.
	write("a.lua", "function one() end\nfunction three() end\n")
	if sum, err := Index(s, root, nil); err != nil || sum.Parsed != 1 || sum.Skipped != 1 {
		t.Fatalf("change sum=%+v err=%v", sum, err)
	}
	if hits, _ := s.SymbolsByName("three", 5); len(hits) != 1 {
		t.Fatalf("new symbol 'three' not indexed: %v", hits)
	}
	// Delete one file -> removed from index.
	os.Remove(filepath.Join(root, "b.lua"))
	if sum, err := Index(s, root, nil); err != nil || sum.Deleted != 1 {
		t.Fatalf("delete sum=%+v err=%v", sum, err)
	}
	if _, ok, _ := s.GetFileByPath("b.lua"); ok {
		t.Fatal("b.lua still indexed after deletion")
	}
}

func TestIndexLanguagesFilterAndAuto(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tool.py"), []byte("def tool():\n    pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := openStore(t)
	sum, err := IndexWithOptions(s, root, Options{Languages: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Indexed != 1 {
		t.Fatalf("go-only indexed = %d, want 1", sum.Indexed)
	}
	if _, ok, _ := s.GetFileByPath("main.go"); !ok {
		t.Fatal("main.go was not indexed")
	}
	if _, ok, _ := s.GetFileByPath("tool.py"); ok {
		t.Fatal("tool.py was indexed despite languages=[go]")
	}

	sum, err = IndexWithOptions(s, root, Options{Languages: []string{"auto"}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Indexed != 2 {
		t.Fatalf("auto indexed = %d, want 2", sum.Indexed)
	}
}

func TestIndexStripsGeneratedAgentsBlockButKeepsUserGuidance(t *testing.T) {
	root := t.TempDir()
	agents := "# House rules\n\nRun the focused tests.\n\n<!-- prowl-agent -->\ngenerated routing instructions\n<!-- /prowl-agent -->\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}
	s := openStore(t)
	if _, err := IndexWithOptions(s, root, Options{Languages: []string{"auto"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetFileByPath("AGENTS.md"); !ok {
		t.Fatal("user-authored AGENTS.md content should remain indexed")
	}
	hits, err := s.SearchChunks("focused tests", 10)
	if err != nil || len(hits) == 0 {
		t.Fatalf("user guidance not searchable: %v %v", hits, err)
	}
	generated, err := s.SearchChunks("generated routing instructions", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) != 0 {
		t.Fatalf("generated Prowl block leaked into index: %+v", generated)
	}
}

func TestSignatureIncludesLanguageSelection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	goSig, err := SignatureWithOptions(root, Options{Languages: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	pySig, err := SignatureWithOptions(root, Options{Languages: []string{"python"}})
	if err != nil {
		t.Fatal(err)
	}
	if goSig == pySig {
		t.Fatal("language selection must affect freshness signature")
	}
}
