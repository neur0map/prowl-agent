package query

import (
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/index"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

func indexed(t *testing.T) *Querier {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := index.Index(s, filepath.Join("..", "..", "testdata", "rice-hypr"), nil); err != nil {
		t.Fatal(err)
	}
	return New(s)
}

func TestFindSymbolCallersCallees(t *testing.T) {
	q := indexed(t)

	sy, err := q.FindSymbol("M.apply")
	if err != nil {
		t.Fatal(err)
	}
	if len(sy) == 0 || sy[0].Name != "M.apply" || sy[0].File != "nvim/lua/opts.lua" {
		t.Fatalf("FindSymbol(M.apply) = %+v", sy)
	}

	callers, err := q.FindCallers("hypr/colors.conf")
	if err != nil {
		t.Fatal(err)
	}
	if len(callers) != 1 || callers[0].File != "hypr/hyprland.conf" {
		t.Fatalf("callers of colors.conf = %+v", callers)
	}

	callees, err := q.FindCallees("hypr/hyprland.conf")
	if err != nil {
		t.Fatal(err)
	}
	var sawInclude bool
	for _, e := range callees {
		if e.Kind == "includes" {
			sawInclude = true
		}
	}
	if !sawInclude {
		t.Fatalf("callees of hyprland.conf missing include edge: %+v", callees)
	}
}

func TestRelationsBlastEntrypoints(t *testing.T) {
	q := indexed(t)

	rel, err := q.FileRelations("waybar/style.css")
	if err != nil || !rel.Exists {
		t.Fatalf("relations err=%v exists=%v", err, rel.Exists)
	}
	if len(rel.Includes) == 0 {
		t.Fatalf("style.css should include colors.css: %+v", rel.Includes)
	}

	blast, err := q.BlastRadius("hypr/colors.conf")
	if err != nil {
		t.Fatal(err)
	}
	if len(blast) != 1 || blast[0].File != "hypr/hyprland.conf" {
		t.Fatalf("blast colors.conf = %+v", blast)
	}

	ep, err := q.EntrypointsFor("hypr/scripts/screenshot.sh")
	if err != nil {
		t.Fatal(err)
	}
	if len(ep) != 1 || ep[0] != "hypr/hyprland.conf" {
		t.Fatalf("entrypoints screenshot.sh = %+v", ep)
	}
}

func TestViolationsHotspotsStatusSimilar(t *testing.T) {
	q := indexed(t)

	v, err := q.ArchitectureViolations()
	if err != nil {
		t.Fatal(err)
	}
	var hardcoded bool
	for _, x := range v {
		if x.Kind == "hardcoded_color" && x.File == "waybar/style.css" {
			hardcoded = true
		}
	}
	if !hardcoded {
		t.Fatalf("expected hardcoded_color violation in style.css, got %+v", v)
	}

	st, err := q.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Counts.Files != 11 {
		t.Fatalf("status files = %d, want 11", st.Counts.Files)
	}

	hs, err := q.RepoHotspots()
	if err != nil {
		t.Fatal(err)
	}
	if len(hs.FanIn) == 0 || len(hs.Largest) == 0 {
		t.Fatalf("hotspots empty: %+v", hs)
	}

	sim, err := q.SimilarCode("workspaces")
	if err != nil {
		t.Fatal(err)
	}
	if len(sim) == 0 {
		t.Fatalf("similar_code(workspaces) empty")
	}

	tf, err := q.TestsFor("hypr/scripts/screenshot.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !tf.Limited || len(tf.Runners) == 0 {
		t.Fatalf("tests_for = %+v", tf)
	}
}
