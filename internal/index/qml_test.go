package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// A QML file that instantiates a repo component should produce a resolved
// `instantiates` edge to the defining .qml file, while built-in types (Column,
// Rectangle) are dropped rather than left dangling.
func TestQMLInstantiationResolves(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Button.qml", "import QtQuick\nRectangle {\n  id: root\n}\n")
	write("Panel.qml", "import QtQuick\nColumn {\n  Button { }\n}\n")

	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := Index(s, dir, nil); err != nil {
		t.Fatal(err)
	}

	panel, ok, _ := s.GetFileByPath("Panel.qml")
	if !ok {
		t.Fatal("Panel.qml not indexed")
	}
	button, _, _ := s.GetFileByPath("Button.qml")

	edges, _ := s.EdgesFromFile(panel.ID, "instantiates")
	if len(edges) != 1 {
		t.Fatalf("Panel instantiates edges = %d, want 1 (Button only; Column dropped)", len(edges))
	}
	if e := edges[0]; e.Raw != "Button" || !e.Resolved || e.DstID != button.ID {
		t.Fatalf("edge = %+v, want resolved Button -> %d", e, button.ID)
	}

	// The file's component symbol is filename-derived.
	syms, _ := s.SymbolsByName("Button", 5)
	found := false
	for _, sy := range syms {
		if sy.File == "Button.qml" && sy.Kind == "component" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Button.qml should have a filename-derived component symbol, got %+v", syms)
	}
}
