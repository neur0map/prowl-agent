package query

import (
	"context"
	"path/filepath"
	"strings"
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
	if _, err := index.Index(s, filepath.Join("..", "..", "testdata", "sample-config"), nil); err != nil {
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

	sim, err := q.SimilarCode(context.Background(), "workspaces")
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

// kwEmbedder maps text to a keyword-presence vector, giving deterministic
// nearest-neighbor behavior without a live model.
type kwEmbedder struct{ kw []string }

func (e kwEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, len(e.kw))
		for j, k := range e.kw {
			if strings.Contains(t, k) {
				v[j] = 1
			}
		}
		out[i] = v
	}
	return out, nil
}

func (kwEmbedder) Generate(context.Context, string) (string, error) { return "", nil }

func (kwEmbedder) Rerank(_ context.Context, _ string, docs []string) ([]int, error) {
	order := make([]int, len(docs))
	for i := range order {
		order[i] = i
	}
	return order, nil
}

// rewritingInf reuses kwEmbedder but returns a fixed query rewrite.
type rewritingInf struct {
	kwEmbedder
	rewrite string
}

func (r rewritingInf) Generate(_ context.Context, _ string) (string, error) { return r.rewrite, nil }

func TestSmartSearch(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mk := func(path, text string) {
		fid, err := s.UpsertFile(store.File{RelPath: path, Lang: "generic", Hash: path, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceFileGraph(fid, nil, nil, nil, []store.Chunk{{StartLine: 1, EndLine: 1, Text: text}}); err != nil {
			t.Fatal(err)
		}
	}
	mk("a.conf", "alpha apple")
	mk("b.conf", "beta banana")
	mk("c.conf", "gamma grape")
	emb := kwEmbedder{kw: []string{"apple", "banana", "grape"}}
	if _, err := index.BuildVectors(context.Background(), s, emb, "kw"); err != nil {
		t.Fatal(err)
	}

	// The fuzzy query "fruit" embeds to nothing; the rewrite to "banana" makes
	// the banana chunk the nearest neighbor.
	inf := rewritingInf{kwEmbedder: emb, rewrite: "banana"}
	res, err := NewWithAssist(s, inf).SmartSearch(context.Background(), "fruit")
	if err != nil {
		t.Fatal(err)
	}
	if res.Rewritten != "banana" {
		t.Fatalf("rewritten = %q, want banana", res.Rewritten)
	}
	if len(res.Matches) == 0 || !strings.Contains(res.Matches[0].Snippet, "banana") {
		t.Fatalf("smart_search top = %+v, want banana first", res.Matches)
	}
}

func TestSimilarCodeHybrid(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mk := func(path, text string) {
		fid, err := s.UpsertFile(store.File{RelPath: path, Lang: "generic", Hash: path, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceFileGraph(fid, nil, nil, nil, []store.Chunk{{StartLine: 1, EndLine: 1, Text: text}}); err != nil {
			t.Fatal(err)
		}
	}
	mk("a.conf", "alpha apple")
	mk("b.conf", "beta banana")
	mk("c.conf", "gamma grape")

	emb := kwEmbedder{kw: []string{"apple", "banana", "grape"}}
	if _, err := index.BuildVectors(context.Background(), s, emb, "kw"); err != nil {
		t.Fatal(err)
	}

	// Hybrid: query nearest the banana chunk.
	hits, err := NewWithAssist(s, emb).SimilarCode(context.Background(), "banana")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Snippet, "banana") {
		t.Fatalf("hybrid top hit = %+v, want banana chunk first", hits)
	}

	// FTS-only fallback still returns results.
	hits2, err := New(s).SimilarCode(context.Background(), "banana")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits2) == 0 {
		t.Fatal("FTS-only SimilarCode returned nothing")
	}
}

func TestOverviewAndClusters(t *testing.T) {
	q := indexed(t)

	ov, err := q.Overview()
	if err != nil {
		t.Fatal(err)
	}
	if ov.Counts.Files != 11 {
		t.Fatalf("overview files = %d, want 11", ov.Counts.Files)
	}
	if len(ov.Entrypoints) == 0 {
		t.Fatal("overview has no entrypoints")
	}
	if len(ov.Palette) == 0 {
		t.Fatal("overview has no color palette")
	}
	if ov.Keybinds == 0 {
		t.Fatal("overview has no keybinds")
	}

	cl, err := q.Clusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(cl) == 0 {
		t.Fatal("no clusters found")
	}
	labels := map[string]bool{}
	for _, c := range cl {
		labels[c.Label] = true
	}
	if !labels["hypr"] && !labels["waybar"] {
		t.Fatalf("expected a hypr or waybar cluster, got %+v", cl)
	}
}
