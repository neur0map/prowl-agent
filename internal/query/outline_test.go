package query

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/index"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestOutlineNestsSymbols(t *testing.T) {
	dir := t.TempDir()
	// A class with two methods (nested) plus a top-level function.
	src := "class Widget:\n    def render(self):\n        return 1\n    def update(self):\n        return 2\n\ndef helper():\n    return 3\n"
	if err := os.WriteFile(filepath.Join(dir, "w.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := index.Index(s, dir, nil); err != nil {
		t.Fatal(err)
	}

	o, err := New(s).Outline("w.py")
	if err != nil {
		t.Fatal(err)
	}
	if o.File != "w.py" {
		t.Fatalf("file = %q, want w.py", o.File)
	}
	byName := map[string]OutlineSymbol{}
	for _, sym := range o.Symbols {
		byName[sym.Name] = sym
	}
	if w, ok := byName["Widget"]; !ok || w.Kind != "class" || w.Depth != 0 {
		t.Fatalf("Widget = %+v, want class at depth 0", w)
	}
	if r, ok := byName["render"]; !ok || r.Depth != 1 {
		t.Fatalf("render = %+v, want a method nested at depth 1", r)
	}
	if h, ok := byName["helper"]; !ok || h.Depth != 0 {
		t.Fatalf("helper = %+v, want a top-level function at depth 0", h)
	}
	// The skeleton carries no bodies: the struct has no content field, and the
	// method's own line range must be its signature+body span, not the file.
	if r := byName["render"]; r.LineStart < 2 || r.LineEnd > 5 {
		t.Fatalf("render line range %d-%d out of the class", r.LineStart, r.LineEnd)
	}

	if _, err := New(s).Outline("does/not/exist.py"); err == nil {
		t.Fatal("expected an error for an unindexed file")
	}
}
