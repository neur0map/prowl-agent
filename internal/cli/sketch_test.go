package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runSketch(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := bindFormatFlags(newSketchCmd())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestSketchCmdPathAndJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Card.qml")
	qml := "import QtQuick\nRectangle {\n  id: card\n  color: \"#111\"\n  Text { text: \"hi\" }\n}\n"
	if err := os.WriteFile(path, []byte(qml), 0o644); err != nil {
		t.Fatal(err)
	}

	// Text render by direct path.
	out, err := runSketch(t, path)
	if err != nil {
		t.Fatalf("sketch: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Card.qml  ·  Rectangle") || !strings.Contains(out, "Text") {
		t.Fatalf("unexpected text sketch:\n%s", out)
	}

	// JSON emits the structured model.
	out, err = runSketch(t, path, "--json")
	if err != nil {
		t.Fatalf("sketch --json: %v\n%s", err, out)
	}
	var sk struct {
		File string `json:"file"`
		Kind string `json:"kind"`
		Root struct {
			ID string `json:"id"`
		} `json:"root"`
	}
	if err := json.Unmarshal([]byte(out), &sk); err != nil {
		t.Fatalf("json parse: %v\n%s", err, out)
	}
	if sk.File != "Card.qml" || sk.Kind != "Rectangle" || sk.Root.ID != "card" {
		t.Fatalf("json model wrong: %+v", sk)
	}
}

func TestSketchCmdErrors(t *testing.T) {
	// A path-like argument that does not exist reports a file error, not a
	// confusing "no symbol named ...".
	if _, err := runSketch(t, "./does-not-exist.qml"); err == nil || !strings.Contains(err.Error(), "file not found") {
		t.Fatalf("expected file-not-found error, got %v", err)
	}
	// An unsupported existing file is rejected with a clear message.
	dir := t.TempDir()
	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runSketch(t, txt); err == nil || !strings.Contains(err.Error(), "visual sketch supports") {
		t.Fatalf("expected unsupported-type error, got %v", err)
	}
}

func TestSketchCmdResolvesTokens(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Tokens.qml"),
		[]byte("pragma Singleton\nimport QtQuick\nSingleton {\n  property int s2: 8\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	card := filepath.Join(dir, "Card.qml")
	if err := os.WriteFile(card,
		[]byte("import QtQuick\nRectangle {\n  spacing: Tokens.s2\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runSketch(t, card)
	if err != nil {
		t.Fatalf("sketch: %v\n%s", err, out)
	}
	if !strings.Contains(out, "spacing=Tokens.s2⟨8⟩") {
		t.Fatalf("token not resolved in output:\n%s", out)
	}
}
