package sketch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTokens(t *testing.T) {
	qml := `import QtQuick
Rectangle {
    spacing: Tokens.s2
    color: Tokens.ink
    width: parent.width
    z: Font.Medium
    radius: Tokens.missing
}
`
	m, err := Of("Card.qml", []byte(qml))
	if err != nil {
		t.Fatal(err)
	}
	sk := m.(*Sketch)
	src := func(name string) ([]byte, bool) {
		switch name {
		case "Tokens":
			return []byte("import QtQuick\nSingleton {\n  property int s2: 8\n  property color ink: Theme.text\n}\n"), true
		case "Theme":
			return []byte("import QtQuick\nSingleton {\n  property color text: \"#cdd6f4\"\n}\n"), true
		}
		return nil, false
	}
	sk.Resolve(src)

	get := func(name string) Prop {
		for _, p := range sk.Root.Props {
			if p.Name == name {
				return p
			}
		}
		t.Fatalf("no prop %q", name)
		return Prop{}
	}
	if r := get("spacing").Resolved; r != "8" {
		t.Errorf("spacing resolved = %q, want 8", r)
	}
	// Alias chain Tokens.ink -> Theme.text -> "#cdd6f4", with quotes stripped.
	if r := get("color").Resolved; r != "#cdd6f4" {
		t.Errorf("color resolved = %q, want #cdd6f4", r)
	}
	// Lowercase-rooted reference is not a singleton token.
	if r := get("width").Resolved; r != "" {
		t.Errorf("width should not resolve, got %q", r)
	}
	// Unknown singleton (a QML enum) is left alone.
	if r := get("z").Resolved; r != "" {
		t.Errorf("Font.Medium should not resolve, got %q", r)
	}
	// A reference to a missing property is left alone.
	if r := get("radius").Resolved; r != "" {
		t.Errorf("Tokens.missing should not resolve, got %q", r)
	}
}

func TestDirSingletonSourceNearest(t *testing.T) {
	root := t.TempDir()
	// Two Theme.qml at different depths; the near one should win.
	far := filepath.Join(root, "other", "Theme.qml")
	near := filepath.Join(root, "app", "ui", "Theme.qml")
	for _, p := range []string{far, near} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(far, []byte("import QtQuick\nSingleton { property color c: \"#000000\" }\n"), 0o644)
	os.WriteFile(near, []byte("import QtQuick\nSingleton { property color c: \"#ffffff\" }\n"), 0o644)

	target := filepath.Join(root, "app", "ui", "Card.qml")
	source := DirSingletonSource(root, target)
	data, ok := source("Theme")
	if !ok {
		t.Fatal("Theme not found")
	}
	if vals := SingletonValues(data); vals["c"] != `"#ffffff"` {
		t.Errorf("nearest Theme not chosen: c = %q, want the app/ui one", vals["c"])
	}
	if _, ok := source("Nope"); ok {
		t.Error("unknown singleton should not resolve")
	}
}
