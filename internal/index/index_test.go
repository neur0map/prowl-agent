package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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

func TestSignatureIncludesIgnorePolicy(t *testing.T) {
	root := t.TempDir()
	first, err := SignatureWithOptions(root, Options{Ignore: []string{"vendor/**"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := SignatureWithOptions(root, Options{Ignore: []string{"generated/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("ignore policy must affect freshness signature even when the walked file set is unchanged")
	}
}

func TestSignatureDetectsSameSizeSameMTimeReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "same.go")
	original := []byte("package a\nfunc Alpha() {}\n")
	replacement := []byte("package a\nfunc Bravo() {}\n")
	if len(original) != len(replacement) {
		t.Fatal("test fixture must preserve size")
	}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1_700_000_000, 123456789)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := SignatureWithOptions(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, replacement, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	after, err := SignatureWithOptions(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("same-size same-mtime content replacement did not change signature")
	}
}

func TestWalkContextCancelsDuringDirectoryOnlyTraversal(t *testing.T) {
	base := t.TempDir()
	root := base
	for i := 0; i < 20; i++ {
		root = filepath.Join(root, "empty")
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ctx := newCancelAfterChecks(8)
	if _, err := WalkContext(ctx, base, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("WalkContext error = %v, want context.Canceled", err)
	}
}

func TestInterruptedIndexIsMarkedIncompleteAndRecovers(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		name := filepath.Join(root, fmt.Sprintf("file_%02d.go", i))
		if err := os.WriteFile(name, []byte("package fixture\nfunc Value() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	database := openStore(t)
	ctx := newCancelAfterChecks(35)
	if _, err := IndexWithOptionsContext(ctx, database, root, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted index error = %v, want context.Canceled", err)
	}
	state, err := database.GetMeta("index_state")
	if err != nil {
		t.Fatal(err)
	}
	if state != "incomplete" {
		t.Fatalf("index_state = %q, want incomplete", state)
	}
	if _, err := IndexWithOptionsContext(context.Background(), database, root, Options{}); err != nil {
		t.Fatal(err)
	}
	state, err = database.GetMeta("index_state")
	if err != nil || state != "complete" {
		t.Fatalf("recovered index_state = %q, %v; want complete", state, err)
	}
}

type cancelAfterChecks struct{ remaining atomic.Int32 }

func newCancelAfterChecks(count int32) *cancelAfterChecks {
	ctx := &cancelAfterChecks{}
	ctx.remaining.Store(count)
	return ctx
}

func (*cancelAfterChecks) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelAfterChecks) Done() <-chan struct{}       { return nil }
func (*cancelAfterChecks) Value(any) any               { return nil }
func (ctx *cancelAfterChecks) Err() error {
	if ctx.remaining.Add(-1) <= 0 {
		return context.Canceled
	}
	return nil
}

func TestValidateSnapshotAcceptsGeneratedAgentsMarker(t *testing.T) {
	root := t.TempDir()
	body := []byte("# Project instructions\n\n<!-- prowl-agent -->\ngenerated material\n<!-- /prowl-agent -->\n\nKeep this human text.\n")
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	s := openStore(t)
	defer s.Close()
	if _, err := IndexWithOptionsContext(context.Background(), s, root, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshotContext(context.Background(), s, root); err != nil {
		t.Fatalf("generated marker snapshot: %v", err)
	}
}
