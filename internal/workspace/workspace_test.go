package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateResolve(t *testing.T) {
	root := t.TempDir()
	w, err := Create(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Path); err != nil {
		t.Fatalf(".prowl not created: %v", err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	w2, err := Resolve(sub)
	if err != nil {
		t.Fatal(err)
	}
	if w2.Root != root {
		t.Fatalf("resolved root = %q, want %q", w2.Root, root)
	}
	if _, err := Resolve(t.TempDir()); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRegistry(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := Register("/x/y", true); err != nil {
		t.Fatal(err)
	}
	if err := Register("/x/y", false); err != nil { // upsert, not duplicate
		t.Fatal(err)
	}
	if err := Register("/a/b", true); err != nil {
		t.Fatal(err)
	}
	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("entries = %d, want 2", len(list))
	}
	for _, e := range list {
		if e.Root == "/x/y" && e.AI {
			t.Fatal("ai flag should have been updated to false")
		}
	}
}

func TestEnsureIgnored(t *testing.T) {
	root := t.TempDir()
	if err := EnsureIgnored(root, ".prowl/"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIgnored(root, ".prowl/"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if strings.Count(string(data), ".prowl/") != 1 {
		t.Fatalf("gitignore should contain .prowl/ once: %q", data)
	}
}
