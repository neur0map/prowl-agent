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

// TestTSConfigPathsExtends covers the Turborepo/Nx pattern: a package tsconfig
// with no paths of its own that extends a shared base config which defines them.
// The base's targets resolve relative to the base config's own directory.
func TestTSConfigPathsExtends(t *testing.T) {
	root := t.TempDir()
	// Base config at repo root with a (suffix-wildcard) alias.
	base := `{ "compilerOptions": { "baseUrl": ".", "paths": { "@shared/*": ["libs/shared/src/*"] } } }`
	if err := os.WriteFile(filepath.Join(root, "tsconfig.base.json"), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	// A package config under apps/web that extends the base and adds no paths.
	appDir := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	child := `{ "extends": "../../tsconfig.base.json", "compilerOptions": { "strict": true } }`
	if err := os.WriteFile(filepath.Join(appDir, "tsconfig.json"), []byte(child), 0o644); err != nil {
		t.Fatal(err)
	}
	got := tsConfigPaths(filepath.Join(appDir, "tsconfig.json"), "apps/web")
	// The base lives at repo root, so @shared/* -> libs/shared/src resolves there,
	// not relative to apps/web.
	if got["@shared/"] == nil || got["@shared/"][0] != "libs/shared/src" {
		t.Errorf("@shared/ -> %v, want [libs/shared/src] (resolved relative to base dir)", got["@shared/"])
	}
}
