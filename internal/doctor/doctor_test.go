package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/graph"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestDoctor(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	mk := func(rel, role string) int64 {
		id, err := s.UpsertFile(store.File{RelPath: rel, Lang: "generic", Role: role, Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := mk("a.conf", "")
	b := mk("b.conf", "")
	theme := mk("themes/t.conf", "theme")
	kb := mk("hypr/hyprland.conf", "wm-config")
	mk("scripts/orphan.sh", "script") // referenced by nobody
	mk("scripts/s.sh", "script")      // referenced by theme (forbidden)

	// cyclic includes: a <-> b
	must(t, s.ReplaceFileGraph(a, nil, nil, []store.RawEdge{{Kind: "includes", Raw: "b.conf", Line: 1}}, nil))
	must(t, s.ReplaceFileGraph(b, nil, nil, []store.RawEdge{{Kind: "includes", Raw: "a.conf", Line: 1}}, nil))
	// theme execs a script (forbidden crossing themes -> scripts)
	must(t, s.ReplaceFileGraph(theme, nil, nil, []store.RawEdge{{Kind: "execs", Raw: "scripts/s.sh", Line: 1}}, nil))
	// duplicate keybinds + broken command + dangling include
	must(t, s.ReplaceFileGraph(kb,
		[]store.Symbol{
			{Name: "$mod Q", Kind: "keybind", StartLine: 1, EndLine: 1},
			{Name: "$mod, Q", Kind: "keybind", StartLine: 2, EndLine: 2},
		}, nil,
		[]store.RawEdge{
			{SrcName: "$mod Q", Kind: "binds", Raw: "prowl_absent_cmd_zzz --x", Line: 1},
			{Kind: "includes", Raw: "missing/x.conf", Line: 3},
		}, nil))

	if err := graph.Resolve(s); err != nil {
		t.Fatal(err)
	}

	rules := config.Rules{Forbid: []config.Forbid{{Name: "no-theme-scripts", From: "themes", To: "scripts"}}}
	rep, err := Run(s, rules, Options{Profile: ProfileRice})
	if err != nil {
		t.Fatal(err)
	}

	has := func(check string) bool {
		for _, f := range rep.Findings {
			if f.Check == check {
				return true
			}
		}
		return false
	}
	for _, c := range []string{
		"cyclic_include", "duplicate_keybind", "broken_command",
		"orphan_script", "dangling_reference", "forbidden_crossing",
	} {
		if !has(c) {
			t.Errorf("doctor missing check %q; findings=%+v", c, rep.Findings)
		}
	}
	if rep.Score >= 100 {
		t.Errorf("score = %d, want < 100 with errors present", rep.Score)
	}
	if rep.Summary["cyclic_include"] == 0 {
		t.Errorf("summary missing cyclic_include: %+v", rep.Summary)
	}

	general, err := Run(s, rules, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, riceOnly := range []string{"duplicate_keybind", "broken_command", "orphan_script", "hardcoded_color"} {
		for _, finding := range general.Findings {
			if finding.Check == riceOnly {
				t.Errorf("general doctor included rice-only check %q: %+v", riceOnly, general.Findings)
			}
		}
	}
	for _, required := range []string{"cyclic_include", "dangling_reference", "forbidden_crossing"} {
		found := false
		for _, finding := range general.Findings {
			found = found || finding.Check == required
		}
		if !found {
			t.Errorf("general doctor missing %q: %+v", required, general.Findings)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestCheckUnindexedLanguages proves doctor flags a language present on disk but
// missing from the index (the silent language-filter misconfiguration), and does
// not warn when that language is indexed.
func TestCheckUnindexedLanguages(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		p := filepath.Join(root, fmt.Sprintf("f%d.go", i))
		if err := os.WriteFile(p, []byte("package p\nfunc F() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	unindexed, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer unindexed.Close()
	got, err := checkUnindexedLanguages(unindexed, Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Check != "unindexed-language" || got[0].Severity != SevWarn {
		t.Fatalf("want one unindexed-language warning, got %+v", got)
	}

	indexed, err := store.Open(filepath.Join(t.TempDir(), "i2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer indexed.Close()
	for i := 0; i < 12; i++ {
		if _, err := indexed.UpsertFile(store.File{RelPath: fmt.Sprintf("f%d.go", i), Lang: "go", Hash: fmt.Sprint(i), Size: 1, MTime: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := checkUnindexedLanguages(indexed, Options{Root: root}); err != nil || len(got) != 0 {
		t.Fatalf("indexed go must not warn: got %+v err %v", got, err)
	}
}
