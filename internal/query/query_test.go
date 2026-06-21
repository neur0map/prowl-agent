package query

import (
	"context"
	"fmt"
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

func TestFindSymbolSubstringFallback(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fid, err := s.UpsertFile(store.File{RelPath: "svc.go", Lang: "go", Hash: "h", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	syms := []store.Symbol{
		{Name: "updateCloudClient", Kind: "function", StartLine: 1, EndLine: 1},
		{Name: "deleteCloudBucket", Kind: "function", StartLine: 2, EndLine: 2},
		{Name: "cloud", Kind: "function", StartLine: 3, EndLine: 3},
		{Name: "localHelper", Kind: "function", StartLine: 4, EndLine: 4},
	}
	if err := s.ReplaceFileGraph(fid, syms, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	q := New(s)

	// "cloud" is a camelCase component the FTS tokenizer keeps whole; the
	// substring fallback must surface both, with the exact "cloud" match first.
	hits, err := q.FindSymbol("Cloud")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(hits))
	for i, h := range hits {
		names[i] = h.Name
	}
	if len(hits) == 0 || hits[0].Name != "cloud" {
		t.Fatalf("FindSymbol(Cloud) = %v, want exact 'cloud' first", names)
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	if !got["updateCloudClient"] || !got["deleteCloudBucket"] {
		t.Fatalf("FindSymbol(Cloud) = %v, want camelCase components included", names)
	}
	if got["localHelper"] {
		t.Fatalf("FindSymbol(Cloud) = %v, must not include unrelated localHelper", names)
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

func TestBlastSummarize(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mk := func(rel string) int64 {
		id, err := s.UpsertFile(store.File{RelPath: rel, Lang: "go", Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	target := mk("pkg/commands/target.go")
	ax := mk("pkg/a/x.go")
	ay := mk("pkg/a/y.go")
	bz := mk("pkg/b/z.go")
	// x and z depend directly on target (depth 1); y depends on x (depth 2).
	if err := s.AddPackageEdges([]store.PkgEdge{
		{FileID: ax, DstFileID: target, Line: 1, Raw: "target"},
		{FileID: bz, DstFileID: target, Line: 1, Raw: "target"},
		{FileID: ay, DstFileID: ax, Line: 1, Raw: "x"},
	}); err != nil {
		t.Fatal(err)
	}

	sum, err := New(s).BlastSummarize("pkg/commands/target.go")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 3 {
		t.Errorf("Total = %d, want 3", sum.Total)
	}
	if sum.Direct != 2 {
		t.Errorf("Direct = %d, want 2", sum.Direct)
	}
	if len(sum.DirectFiles) != 2 || sum.DirectFiles[0] != "pkg/a/x.go" || sum.DirectFiles[1] != "pkg/b/z.go" {
		t.Errorf("DirectFiles = %v, want [pkg/a/x.go pkg/b/z.go]", sum.DirectFiles)
	}
	// pkg/a holds x (depth 1) and y (depth 2) -> count 2, leads pkg/b (count 1).
	if len(sum.BySubsystem) != 2 {
		t.Fatalf("BySubsystem = %+v, want 2 groups", sum.BySubsystem)
	}
	if sum.BySubsystem[0].Subsystem != "pkg/a" || sum.BySubsystem[0].Count != 2 {
		t.Errorf("BySubsystem[0] = %+v, want {pkg/a 2}", sum.BySubsystem[0])
	}
	if sum.BySubsystem[1].Subsystem != "pkg/b" || sum.BySubsystem[1].Count != 1 {
		t.Errorf("BySubsystem[1] = %+v, want {pkg/b 1}", sum.BySubsystem[1])
	}
}

func TestSplitBlobCluster(t *testing.T) {
	// A single dominant component is subdivided by directory subsystem.
	langOf := map[string]string{}
	var blob []string
	for i := 0; i < 30; i++ {
		p := fmt.Sprintf("pkg/gui/f%d.go", i)
		if i >= 20 {
			p = fmt.Sprintf("pkg/commands/f%d.go", i)
		}
		blob = append(blob, p)
		langOf[p] = "go"
	}
	out := splitBlobCluster([]Cluster{{Label: "pkg", Lang: "go", Files: blob}}, langOf)
	got := map[string]int{}
	for _, c := range out {
		got[c.Label] = len(c.Files)
	}
	if got["pkg/gui"] != 20 || got["pkg/commands"] != 10 {
		t.Fatalf("subdivision = %v, want pkg/gui:20 pkg/commands:10", got)
	}

	// Several balanced components are left unchanged (e.g. config include trees).
	balanced := []Cluster{
		{Label: "a", Files: []string{"a/1", "a/2", "a/3"}},
		{Label: "b", Files: []string{"b/1", "b/2"}},
	}
	out2 := splitBlobCluster(balanced, map[string]string{})
	if len(out2) != 2 || out2[0].Label != "a" || out2[1].Label != "b" {
		t.Fatalf("balanced clusters should be unchanged, got %+v", out2)
	}
}

func TestGuideDocs(t *testing.T) {
	files := []store.File{
		{RelPath: "README.md", Lang: "markdown"},
		{RelPath: "src/main.go", Lang: "go"},
		{RelPath: "docs/dev/Codebase_Guide.md", Lang: "markdown"},
		{RelPath: "docs/Config.md", Lang: "markdown"},
		{RelPath: "CONTRIBUTING.md", Lang: "markdown"},
		{RelPath: "docs/architecture/overview.md", Lang: "markdown"},
	}
	got := guideDocs(files)
	want := []string{
		"README.md",
		"CONTRIBUTING.md",
		"docs/architecture/overview.md",
		"docs/dev/Codebase_Guide.md",
	}
	if len(got) != len(want) {
		t.Fatalf("guideDocs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("guideDocs = %v, want %v", got, want)
		}
	}
	for _, g := range got {
		if g == "docs/Config.md" || g == "src/main.go" {
			t.Errorf("guideDocs included non-guide %q", g)
		}
	}
}
