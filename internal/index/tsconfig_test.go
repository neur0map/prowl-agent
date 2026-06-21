package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTSConfigPathsJSONC(t *testing.T) {
	dir := t.TempDir()
	// A real-world tsconfig: line + block comments, trailing commas, baseUrl.
	body := `{
  // project config
  "compilerOptions": {
    "baseUrl": ".",
    "paths": {
      "@/*": ["./src/*"], /* app alias */
      "@components/*": ["src/components/*"],
    },
  },
}`
	p := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := tsConfigPaths(p, "web")
	if got["@/"] == nil || got["@/"][0] != "web/src" {
		t.Errorf("@/ -> %v, want [web/src]", got["@/"])
	}
	if got["@components/"] == nil || got["@components/"][0] != "web/src/components" {
		t.Errorf("@components/ -> %v, want [web/src/components]", got["@components/"])
	}
}

func TestStripJSONC(t *testing.T) {
	in := `{
  "a": 1, // trailing comment
  /* block */ "b": "x/*not a comment*/y",
  "c": [1, 2,],
}`
	out := string(stripJSONC([]byte(in)))
	// The string value's contents (including /* */) must be preserved.
	if want := `"x/*not a comment*/y"`; !contains(out, want) {
		t.Errorf("string literal corrupted: %s", out)
	}
	// Comments removed.
	if contains(out, "trailing comment") || contains(out, "block */ ") {
		t.Errorf("comments not stripped: %s", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
