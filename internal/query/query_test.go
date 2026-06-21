package query

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/graph"
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
	if ep.Count != 1 || len(ep.Entrypoints) != 1 || ep.Entrypoints[0] != "hypr/hyprland.conf" {
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

func TestFindSymbolRanksCodeOverConfigAndVendored(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Three symbols named "build": a config setting and a vendored function
	// both sort alphabetically before the project's own code, so only ranking
	// (not the alphabetical exact-match order) can float the code definition up.
	mk := func(rel, kind string) {
		fid, err := s.UpsertFile(store.File{RelPath: rel, Lang: "go", Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceFileGraph(fid, []store.Symbol{{Name: "build", Kind: kind, StartLine: 1, EndLine: 1}}, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	mk("aaa_config.yml", "setting")
	mk("vendor/dep/lib.go", "function")
	mk("zzz_app.go", "function")

	hits, err := New(s).FindSymbol("build")
	if err != nil {
		t.Fatal(err)
	}
	files := make([]string, len(hits))
	for i, h := range hits {
		files[i] = h.File
	}
	want := []string{"zzz_app.go", "aaa_config.yml", "vendor/dep/lib.go"}
	if len(files) != len(want) {
		t.Fatalf("FindSymbol(build) files = %v, want %v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("FindSymbol(build) order = %v, want %v (code, then config, then vendored)", files, want)
		}
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

func TestIsVendored(t *testing.T) {
	vendored := []string{
		"vendor/github.com/x/y/z.go",
		"pkg/sub/vendor/lib.go",
		"third_party/foo.c",
		"node_modules/dep/index.js",
		"api/service.pb.go",
		"proto/msg_pb2.py",
	}
	for _, p := range vendored {
		if !isVendored(p) {
			t.Errorf("isVendored(%q) = false, want true", p)
		}
	}
	project := []string{
		"pkg/gui/gui.go",
		"src/main.go",
		"internal/vendored_thing.go", // "vendored" is not a path segment "vendor"
		"cmd/app/main.go",
	}
	for _, p := range project {
		if isVendored(p) {
			t.Errorf("isVendored(%q) = true, want false", p)
		}
	}
}

func TestClusterLabel(t *testing.T) {
	// Monorepo: files under packages/zod label as "packages/zod", not "packages".
	if got := clusterLabel([]string{"packages/zod/src/a.ts", "packages/zod/src/b.ts"}); got != "packages/zod" {
		t.Errorf("clusterLabel monorepo = %q, want packages/zod", got)
	}
	// Config repo: two-segment paths label by the first segment.
	if got := clusterLabel([]string{"hypr/a.conf", "hypr/b.conf"}); got != "hypr" {
		t.Errorf("clusterLabel config = %q, want hypr", got)
	}
	// Root-only files have no subsystem.
	if got := clusterLabel([]string{"README.md", "LICENSE"}); got != "misc" {
		t.Errorf("clusterLabel root = %q, want misc", got)
	}
}

func TestFindReferencesFallback(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	def, err := s.UpsertFile(store.File{RelPath: "svc.go", Lang: "go", Hash: "a", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFileGraph(def,
		[]store.Symbol{{Name: "DoThing", Kind: "function", StartLine: 10, EndLine: 12}},
		nil, nil,
		[]store.Chunk{{StartLine: 10, EndLine: 12, Text: "func DoThing() error { return nil }"}}); err != nil {
		t.Fatal(err)
	}
	user, err := s.UpsertFile(store.File{RelPath: "user.go", Lang: "go", Hash: "b", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFileGraph(user,
		[]store.Symbol{{Name: "run", Kind: "function", StartLine: 1, EndLine: 3}},
		nil, nil,
		[]store.Chunk{{StartLine: 1, EndLine: 3, Text: "func run() { _ = DoThing() }"}}); err != nil {
		t.Fatal(err)
	}

	hits, err := s.SymbolsByName("DoThing", 1)
	if err != nil || len(hits) == 0 {
		t.Fatalf("SymbolsByName(DoThing) = %v, %v", hits, err)
	}
	u, err := New(s).FindReferences(hits[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Symbol != "DoThing" {
		t.Errorf("Symbol = %q, want DoThing", u.Symbol)
	}
	// No symbol-reference edges -> falls back to call sites, excluding the def,
	// pinpointing the exact usage line, its text, and the enclosing function.
	var userSite *CallSite
	for i := range u.CallSites {
		if u.CallSites[i].File == "svc.go" {
			t.Errorf("CallSites = %+v, must exclude the definition in svc.go", u.CallSites)
		}
		if u.CallSites[i].File == "user.go" {
			userSite = &u.CallSites[i]
		}
	}
	if userSite == nil {
		t.Fatalf("CallSites = %+v, want a usage in user.go", u.CallSites)
	}
	if userSite.Line != 1 || !strings.Contains(userSite.Text, "DoThing()") {
		t.Errorf("user.go call site = %+v, want line 1 with the DoThing() call text", *userSite)
	}
	if userSite.In != "run" {
		t.Errorf("user.go call site In = %q, want enclosing function run", userSite.In)
	}
}

// TestOverviewCapsEntrypoints guards the token-economy fix: on a codebase with
// many root files, overview reports the true entrypoint count but caps the
// inline sample so the first-call answer stays small.
func TestOverviewCapsEntrypoints(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mk := func(rel string) int64 {
		id, err := s.UpsertFile(store.File{RelPath: rel, Lang: "lua", Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	mk("core.lua") // imported by all -> not an entrypoint
	const n = 25
	for i := 0; i < n; i++ {
		id := mk(fmt.Sprintf("m%02d.lua", i))
		if err := s.ReplaceFileGraph(id, nil, nil, []store.RawEdge{{Kind: "includes", Raw: "core", Line: 1}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := graph.Resolve(s); err != nil {
		t.Fatal(err)
	}
	o, err := New(s).Overview()
	if err != nil {
		t.Fatal(err)
	}
	if o.EntrypointCount != n {
		t.Fatalf("EntrypointCount = %d, want %d", o.EntrypointCount, n)
	}
	if len(o.Entrypoints) > 20 {
		t.Fatalf("entrypoints sample = %d, want at most 20", len(o.Entrypoints))
	}
	// EntrypointsFor on the hub reports the full count but caps its sample too.
	ep, err := New(s).EntrypointsFor("core.lua")
	if err != nil {
		t.Fatal(err)
	}
	if ep.Count != n {
		t.Fatalf("EntrypointsFor count = %d, want %d", ep.Count, n)
	}
	if len(ep.Entrypoints) > 20 {
		t.Fatalf("EntrypointsFor sample = %d, want at most 20", len(ep.Entrypoints))
	}
}

func TestSearchChunksDemotesVendored(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// The dense vendored chunk repeats the term, so FTS ranks it above the lone
	// project hit; demotion must still surface the project file first.
	mk := func(rel, text string) {
		fid, err := s.UpsertFile(store.File{RelPath: rel, Lang: "go", Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceFileGraph(fid, nil, nil, nil,
			[]store.Chunk{{StartLine: 1, EndLine: 1, Text: text}}); err != nil {
			t.Fatal(err)
		}
	}
	mk("vendor/dep/consts.go", "status status status status status status status status")
	mk("app/status.go", "func refresh() { return status }")

	hits, err := New(s).SimilarCode(context.Background(), "status")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 {
		t.Fatalf("SimilarCode(status) = %d hits, want both files", len(hits))
	}
	if hits[0].File != "app/status.go" {
		t.Errorf("SimilarCode(status)[0].File = %q, want the project file first (vendored demoted)", hits[0].File)
	}
	if !strings.HasPrefix(hits[len(hits)-1].File, "vendor/") {
		t.Errorf("SimilarCode(status) last = %q, want vendored demoted to the end", hits[len(hits)-1].File)
	}
}

func TestFindReferencesExcludesSiblingDefs(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	def := func(rel string, start int) {
		fid, err := s.UpsertFile(store.File{RelPath: rel, Lang: "go", Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceFileGraph(fid,
			[]store.Symbol{{Name: "Handle", Kind: "function", StartLine: start, EndLine: start + 1}},
			nil, nil,
			[]store.Chunk{{StartLine: start, EndLine: start + 1, Text: "func Handle() error { return nil }"}}); err != nil {
			t.Fatal(err)
		}
	}
	def("a.go", 10) // queried definition
	def("b.go", 5)  // a sibling definition of the same name
	cid, err := s.UpsertFile(store.File{RelPath: "c.go", Lang: "go", Hash: "c", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFileGraph(cid,
		[]store.Symbol{{Name: "run", Kind: "function", StartLine: 1, EndLine: 3}},
		nil, nil,
		[]store.Chunk{{StartLine: 1, EndLine: 3, Text: "func run() { _ = Handle() }"}}); err != nil {
		t.Fatal(err)
	}

	hits, err := s.SymbolsByName("Handle", 5)
	if err != nil {
		t.Fatal(err)
	}
	var aID int64
	for _, h := range hits {
		if h.File == "a.go" {
			aID = h.ID
		}
	}
	u, err := New(s).FindReferences(aID)
	if err != nil {
		t.Fatal(err)
	}
	var calledInRun bool
	for _, c := range u.CallSites {
		if c.File == "a.go" || c.File == "b.go" {
			t.Errorf("call site %+v is a Handle definition, not a usage; must be excluded", c)
		}
		if c.File == "c.go" && c.In == "run" {
			calledInRun = true
		}
	}
	if !calledInRun {
		t.Fatalf("want the c.go usage tagged with enclosing run, got %+v", u.CallSites)
	}
}

func TestFindReferencesCallableMatchesCallsOnly(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// A method named Save, plus a caller file that both calls it (`s.Save()`)
	// and merely names it as a struct field (`Save: true`). Only the call is a
	// blast-radius hit.
	def, err := s.UpsertFile(store.File{RelPath: "svc.go", Lang: "go", Hash: "a", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFileGraph(def,
		[]store.Symbol{{Name: "Save", Kind: "method", StartLine: 10, EndLine: 12}},
		nil, nil,
		[]store.Chunk{{StartLine: 10, EndLine: 12, Text: "func (s *S) Save() error { return nil }"}}); err != nil {
		t.Fatal(err)
	}
	user, err := s.UpsertFile(store.File{RelPath: "user.go", Lang: "go", Hash: "b", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFileGraph(user,
		[]store.Symbol{{Name: "run", Kind: "function", StartLine: 1, EndLine: 4}},
		nil, nil,
		[]store.Chunk{{StartLine: 1, EndLine: 4, Text: "func run(o Opts) {\n\tcfg := Opts{Save: true}\n\ts.Save()\n}"}}); err != nil {
		t.Fatal(err)
	}

	hits, err := s.SymbolsByName("Save", 5)
	if err != nil || len(hits) == 0 {
		t.Fatalf("SymbolsByName(Save) = %v, %v", hits, err)
	}
	u, err := New(s).FindReferences(hits[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.CallSites) != 1 {
		t.Fatalf("call sites = %+v, want exactly the one call (Save: true is a field, not a call)", u.CallSites)
	}
	if u.CallSites[0].Line != 3 || u.CallSites[0].In != "run" {
		t.Errorf("call site = %+v, want line 3 inside run", u.CallSites[0])
	}
}

func TestSearchChunksRanksSourceOverTestsAndDocs(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mk := func(rel, text string) {
		fid, err := s.UpsertFile(store.File{RelPath: rel, Lang: "go", Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceFileGraph(fid, nil, nil, nil, []store.Chunk{{StartLine: 1, EndLine: 1, Text: text}}); err != nil {
			t.Fatal(err)
		}
	}
	// The vendored chunk matches the term most (highest FTS rank) and the doc
	// matches twice, but the project's source must still lead, then the test,
	// then the doc, then vendored.
	mk("vendor/dep/lib.go", "widget widget widget widget")
	mk("docs/guide.md", "the widget renders the widget")
	mk("app/core_test.go", "func TestWidget() { widget }")
	mk("app/core.go", "func render() { widget }")

	hits, err := New(s).SimilarCode(context.Background(), "widget")
	if err != nil {
		t.Fatal(err)
	}
	files := make([]string, len(hits))
	for i, h := range hits {
		files[i] = h.File
	}
	want := []string{"app/core.go", "app/core_test.go", "docs/guide.md", "vendor/dep/lib.go"}
	if len(files) != len(want) {
		t.Fatalf("search(widget) files = %v, want %v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("search(widget) order = %v, want source, then test, then doc, then vendored", files)
		}
	}
}

func TestTestsForFindsColocatedTests(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, rel := range []string{"pkg/os.go", "pkg/os_test.go", "pkg/cmd_test.go", "pkg/helper.go", "other/x_test.go"} {
		if _, err := s.UpsertFile(store.File{RelPath: rel, Lang: "go", Hash: rel, Size: 1, MTime: 1}); err != nil {
			t.Fatal(err)
		}
	}
	tf, err := New(s).TestsFor("pkg/os.go")
	if err != nil {
		t.Fatal(err)
	}
	if tf.Limited {
		t.Errorf("TestsFor(os.go) limited=true, want false when colocated tests exist: %+v", tf)
	}
	if len(tf.Tests) != 2 {
		t.Fatalf("TestsFor(os.go) tests = %v, want the 2 colocated test files (not helper.go, not other/)", tf.Tests)
	}
	if tf.Tests[0] != "pkg/os_test.go" {
		t.Errorf("TestsFor(os.go) first = %q, want pkg/os_test.go (conventional match first)", tf.Tests[0])
	}
	for _, x := range tf.Tests {
		if x == "other/x_test.go" {
			t.Errorf("TestsFor(os.go) wrongly included a test from another directory: %v", tf.Tests)
		}
	}
}
