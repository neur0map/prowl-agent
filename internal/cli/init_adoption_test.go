package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitWarnsOnUnindexedDominantLanguage proves the silent-empty-index
// foot-gun is caught: when the language filter excludes the repo's real stack,
// init/status surface an actionable warning, and `--languages auto` clears it.
func TestInitWarnsOnUnindexedDominantLanguage(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		p := filepath.Join(root, fmt.Sprintf("f%d.go", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("package main\n\nfunc F%d() {}\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate a copied config that excludes Go.
	if _, err := RunInit(InitOptions{Root: root, Languages: []string{"lua"}, LanguagesSet: true, IntegrationsSet: true}); err != nil {
		t.Fatal(err)
	}
	warns := unindexedLanguageWarnings(root)
	if len(warns) == 0 || !strings.Contains(warns[0], "go") {
		t.Fatalf("expected an unindexed-go warning, got %v", warns)
	}

	// The one-command fix clears it.
	if _, err := RunInit(InitOptions{Root: root, Languages: []string{"auto"}, LanguagesSet: true, IntegrationsSet: true}); err != nil {
		t.Fatal(err)
	}
	if warns := unindexedLanguageWarnings(root); len(warns) != 0 {
		t.Fatalf("warning persisted after --languages auto: %v", warns)
	}
}

func TestParseLanguagesFlag(t *testing.T) {
	if langs, set := parseLanguagesFlag("  "); set || langs != nil {
		t.Errorf("blank should be unset: %v %v", langs, set)
	}
	if langs, set := parseLanguagesFlag("auto"); !set || len(langs) != 1 || langs[0] != "auto" {
		t.Errorf("auto: %v %v", langs, set)
	}
	if langs, set := parseLanguagesFlag("go, ts ,py"); !set || len(langs) != 3 || langs[2] != "py" {
		t.Errorf("list parse: %v %v", langs, set)
	}
}

// TestLanguageFilterMostlyExcludes proves the auto-heal fires only when a filter
// excludes the majority of the repo's code (a stale/copied config), and leaves a
// deliberately narrow filter that keeps the majority untouched.
func TestLanguageFilterMostlyExcludes(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("g%d.go", i)), []byte("package m\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !languageFilterMostlyExcludes(root, nil, []string{"lua"}) {
		t.Error("expected heal: filter excludes the Go majority")
	}
	if languageFilterMostlyExcludes(root, nil, []string{"go"}) {
		t.Error("no heal expected: filter keeps the majority")
	}
	if languageFilterMostlyExcludes(root, nil, []string{"auto"}) {
		t.Error("auto must never heal")
	}
	if languageFilterMostlyExcludes(root, nil, nil) {
		t.Error("empty filter must not heal")
	}
}
