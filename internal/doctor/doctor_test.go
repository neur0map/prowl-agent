package doctor

import (
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
	rep, err := Run(s, rules, Options{})
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
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
