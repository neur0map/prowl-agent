// Package query implements the 12 structural queries Prowl Agent exposes to
// agents. All results are deterministic and carry file:line provenance.
package query

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/assist"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

// Querier answers structural queries against an index.
type Querier struct {
	s   *store.Store
	inf assist.Inferencer // optional; when set, SimilarCode is hybrid semantic
}

// New wraps a store for structural (FTS-only) queries.
func New(s *store.Store) *Querier { return &Querier{s: s} }

// NewWithAssist wraps a store with a local inferencer for hybrid semantic search.
func NewWithAssist(s *store.Store, inf assist.Inferencer) *Querier {
	return &Querier{s: s, inf: inf}
}

// DefaultLimit bounds result sizes.
const DefaultLimit = 50

func (q *Querier) fileID(path string) (int64, bool, error) {
	f, ok, err := q.s.GetFileByPath(path)
	if err != nil || !ok {
		return 0, false, err
	}
	return f.ID, true, nil
}

// FindSymbol returns exact-name matches first, then FTS matches, then a
// substring fallback that catches camelCase/snake_case components (e.g.
// "cloud" finding "updateCloudClient") which the FTS tokenizer keeps whole.
// Results are then stably ranked so project code definitions outrank
// config/doc entries (settings, headings) and project files outrank vendored
// or generated ones, while the match-quality order is kept within each tier.
func (q *Querier) FindSymbol(name string) ([]store.SymbolHit, error) {
	exact, err := q.s.SymbolsByName(name, DefaultLimit)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool, len(exact))
	out := make([]store.SymbolHit, 0, len(exact))
	add := func(hits []store.SymbolHit) {
		for _, h := range hits {
			if !seen[h.ID] {
				seen[h.ID] = true
				out = append(out, h)
			}
		}
	}
	add(exact)
	if fts, err := q.s.SearchSymbols(name, DefaultLimit); err == nil {
		add(fts)
	}
	if len(out) < DefaultLimit {
		if sub, err := q.s.SymbolsBySubstring(name, DefaultLimit); err == nil {
			add(sub)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return findRank(out[i]) < findRank(out[j])
	})
	return out, nil
}

// Usages is where a symbol is referenced: graph edges for config/resource
// references, or, for code symbols (which have no language-level call graph),
// full-text call sites of the symbol's name.
type Usages struct {
	Symbol    string     `json:"symbol"`
	Edges     []EdgeView `json:"edges,omitempty"`
	CallSites []CallSite `json:"call_sites,omitempty"`
	Note      string     `json:"note,omitempty"`
}

// CallSite is a precise source location where a symbol name appears: the file,
// the line, that line's text, and the enclosing function or type (`in`) when
// the usage sits inside one, so the agent sees which functions call a symbol
// instead of a 40-line chunk.
type CallSite struct {
	File string `json:"file"`
	In   string `json:"in"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// FindReferences returns where a symbol is used. It prefers symbol-reference
// graph edges (config variables, shared resources); when there are none -- the
// usual case for code, since prowl has no call graph -- it falls back to
// full-text usages of the symbol name, excluding the definition's own chunk, so
// `find <name>` -> `references <id>` answers "what calls this" instead of empty.
func (q *Querier) FindReferences(symbolID int64) (Usages, error) {
	var u Usages
	sym, ok, err := q.s.SymbolByID(symbolID)
	if err != nil {
		return u, err
	}
	if ok {
		u.Symbol = sym.Name
	}
	edges, err := q.s.IncomingEdges("symbol", symbolID)
	if err != nil {
		return u, err
	}
	if len(edges) > 0 {
		u.Edges = edgeViews(edges)
		return u, nil
	}
	if !ok || sym.Name == "" {
		return u, nil
	}
	chunks, err := q.s.SearchChunkText(sym.Name, DefaultLimit)
	if err != nil {
		return u, err
	}
	const maxSites = 40
	for _, ch := range chunks {
		for i, ln := range strings.Split(ch.Text, "\n") {
			abs := ch.StartLine + i
			if ch.File == sym.File && sym.Line <= abs && abs <= sym.EndLine {
				continue // the definition's own body, not a usage
			}
			if !containsWord(ln, sym.Name) {
				continue // name appears only as a substring of a longer identifier
			}
			u.CallSites = append(u.CallSites, CallSite{File: ch.File, Line: abs, Text: strings.TrimSpace(ln)})
		}
		if len(u.CallSites) >= maxSites {
			u.CallSites = u.CallSites[:maxSites]
			break
		}
	}
	spansByFile := make(map[string][]store.SymbolSpan)
	for i := range u.CallSites {
		cs := &u.CallSites[i]
		spans, ok := spansByFile[cs.File]
		if !ok {
			spans, _ = q.s.SymbolSpans(cs.File)
			spansByFile[cs.File] = spans
		}
		if in := enclosingName(spans, cs.Line); in != "" && in != sym.Name {
			cs.In = in
		}
	}
	if len(u.CallSites) > 0 {
		u.Note = "call_sites are name usages found in source (no language call graph); some may be comments or docs"
	}
	return u, nil
}

// enclosingName returns the innermost code definition whose line range contains
// line, or "" when the line sits at file scope. Auxiliary config/doc kinds are
// skipped, and the tightest range wins so a method outranks its enclosing type.
func enclosingName(spans []store.SymbolSpan, line int) string {
	name, best := "", 0
	for _, sp := range spans {
		if auxiliaryKind(sp.Kind) || sp.StartLine > line || line > sp.EndLine {
			continue
		}
		if size := sp.EndLine - sp.StartLine; name == "" || size < best {
			name, best = sp.Name, size
		}
	}
	return name
}

// containsWord reports whether name appears in line as a whole identifier token,
// not as a substring of a longer identifier.
func containsWord(line, name string) bool {
	for from := 0; from <= len(line)-len(name); {
		i := strings.Index(line[from:], name)
		if i < 0 {
			return false
		}
		i += from
		beforeOK := i == 0 || !isIdentByte(line[i-1])
		end := i + len(name)
		afterOK := end >= len(line) || !isIdentByte(line[end])
		if beforeOK && afterOK {
			return true
		}
		from = i + 1
	}
	return false
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// depKinds are the dependency edges traversed for callers and blast radius,
// including the synthetic "pkg" fan-out (an importer to every file of an
// imported package). calleeKinds omits "pkg": for "what does this file import"
// the direct includes/execs/binds are the answer, and the pkg fan-out would
// repeat every file of every imported package.
var depKinds = []string{"includes", "execs", "binds", "autostarts", "references", "pkg"}
var calleeKinds = []string{"includes", "execs", "binds", "autostarts", "references"}

// EdgeView is the agent-facing shape of a dependency edge: the related file, the
// kind, the line, the literal target, and whether it resolved. It drops the
// internal node ids/types so the result stays a uniform TOON table even when a
// file mixes resolved (in-project) and unresolved (external) edges.
type EdgeView struct {
	File     string `json:"file"`
	Kind     string `json:"kind"`
	Line     int    `json:"line"`
	Raw      string `json:"raw"`
	Resolved bool   `json:"resolved"`
}

func edgeViews(rows []store.EdgeRow) []EdgeView {
	out := make([]EdgeView, len(rows))
	for i, e := range rows {
		out[i] = EdgeView{File: e.File, Kind: e.Kind, Line: e.Line, Raw: e.Raw, Resolved: e.Resolved}
	}
	return out
}

// FindCallers returns configs/scripts that include, exec, or bind to a file
// (including cross-package importers via the synthetic pkg edge).
func (q *Querier) FindCallers(path string) ([]EdgeView, error) {
	id, ok, err := q.fileID(path)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := q.s.IncomingEdges("file", id, depKinds...)
	return edgeViews(rows), err
}

// FindCallees returns what a file directly includes, execs, or binds to.
func (q *Querier) FindCallees(path string) ([]EdgeView, error) {
	id, ok, err := q.fileID(path)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := q.s.EdgesFromFile(id, calleeKinds...)
	return edgeViews(rows), err
}

// Relations is the neighborhood of a file.
type Relations struct {
	File       string            `json:"file"`
	Exists     bool              `json:"exists"`
	Symbols    []store.SymbolHit `json:"symbols"`
	Includes   []EdgeView        `json:"includes"`
	IncludedBy []EdgeView        `json:"included_by"`
}

// FileRelations returns a file's symbols and include neighbors.
func (q *Querier) FileRelations(path string) (Relations, error) {
	r := Relations{File: path}
	id, ok, err := q.fileID(path)
	if err != nil || !ok {
		return r, err
	}
	r.Exists = true
	r.Symbols, _ = q.s.SymbolsInFile(id)
	inc, _ := q.s.EdgesFromFile(id, "includes")
	r.Includes = edgeViews(inc)
	by, _ := q.s.IncomingEdges("file", id, "includes")
	r.IncludedBy = edgeViews(by)
	return r, nil
}

// BlastRadius returns the full list of files that transitively depend on a file.
func (q *Querier) BlastRadius(path string) ([]store.Dep, error) {
	id, ok, err := q.fileID(path)
	if err != nil || !ok {
		return nil, err
	}
	return q.s.TransitiveDependents(id)
}

// SubsystemCount pairs a subsystem (top directory) with its dependent count.
type SubsystemCount struct {
	Subsystem string `json:"subsystem"`
	Count     int    `json:"count"`
}

// BlastSummary is a token-lean blast-radius overview: the total dependent count,
// a breakdown by subsystem (which reveals the dependency hubs that drive the
// radius), and the direct importers (the actionable depth-1 set). The full file
// list is available with --all. The graph is package-granular for code, so this
// counts files that compile-depend on the target, not symbol-level callers.
type BlastSummary struct {
	File        string           `json:"file"`
	Total       int              `json:"total"`
	Direct      int              `json:"direct"`
	BySubsystem []SubsystemCount `json:"by_subsystem"`
	DirectFiles []string         `json:"direct_files"`
}

// BlastSummarize returns a grouped blast-radius summary instead of the full list.
func (q *Querier) BlastSummarize(path string) (BlastSummary, error) {
	id, ok, err := q.fileID(path)
	if err != nil || !ok {
		return BlastSummary{File: path}, err
	}
	deps, err := q.s.TransitiveDependents(id)
	if err != nil {
		return BlastSummary{File: path}, err
	}
	sum := BlastSummary{File: path, Total: len(deps)}
	byDir := map[string]int{}
	for _, d := range deps {
		byDir[subsystem(d.File)]++
		if d.Depth == 1 {
			sum.Direct++
			sum.DirectFiles = append(sum.DirectFiles, d.File)
		}
	}
	for dir, n := range byDir {
		sum.BySubsystem = append(sum.BySubsystem, SubsystemCount{Subsystem: dir, Count: n})
	}
	sort.Slice(sum.BySubsystem, func(i, j int) bool {
		if sum.BySubsystem[i].Count != sum.BySubsystem[j].Count {
			return sum.BySubsystem[i].Count > sum.BySubsystem[j].Count
		}
		return sum.BySubsystem[i].Subsystem < sum.BySubsystem[j].Subsystem
	})
	if len(sum.BySubsystem) > 15 {
		sum.BySubsystem = sum.BySubsystem[:15]
	}
	sort.Strings(sum.DirectFiles)
	// The Direct count above is the full depth-1 total; inline only a sample so a
	// hub file (hundreds of direct importers) does not balloon the summary. The
	// complete list is available with --all.
	if len(sum.DirectFiles) > 20 {
		sum.DirectFiles = sum.DirectFiles[:20]
	}
	return sum, nil
}

// subsystem maps a file to a coarse subsystem label: its first two path segments
// (e.g. pkg/gui), or the first segment / "." for shallow paths.
func subsystem(file string) string {
	parts := strings.Split(file, "/")
	switch len(parts) {
	case 1:
		return "."
	case 2:
		return parts[0]
	default:
		return parts[0] + "/" + parts[1]
	}
}

// EntrypointSet is the set of root files from which a file is reachable: the full
// count plus a shallow-first sample. A widely-used utility is reachable from
// nearly every root, so the inline list is capped to keep the answer token-lean.
type EntrypointSet struct {
	File        string   `json:"file"`
	Count       int      `json:"count"`
	Entrypoints []string `json:"entrypoints"`
}

// EntrypointsFor returns the root files (no incoming dependency edges) from which
// path is reachable, as a count plus a shallow-first sample.
func (q *Querier) EntrypointsFor(path string) (EntrypointSet, error) {
	out := EntrypointSet{File: path}
	id, ok, err := q.fileID(path)
	if err != nil || !ok {
		return out, err
	}
	deps, err := q.s.TransitiveDependents(id)
	if err != nil {
		return out, err
	}
	if len(deps) == 0 {
		out.Count, out.Entrypoints = 1, []string{path} // nothing depends on it -> it is the entrypoint
		return out, nil
	}
	var roots []string
	for _, d := range deps {
		did, err := q.s.FileID(d.File)
		if err != nil {
			continue
		}
		if in, _ := q.s.IncomingEdges("file", did, depKinds...); len(in) == 0 {
			roots = append(roots, d.File)
		}
	}
	out.Count = len(roots)
	sort.Slice(roots, func(i, j int) bool {
		if a, b := strings.Count(roots[i], "/"), strings.Count(roots[j], "/"); a != b {
			return a < b
		}
		return roots[i] < roots[j]
	})
	if len(roots) > 20 {
		roots = roots[:20]
	}
	out.Entrypoints = roots
	return out, nil
}

// TestsResult is the (deliberately limited) analogue of tests_for.
type TestsResult struct {
	Limited bool            `json:"limited"`
	Note    string          `json:"note"`
	Runners []store.EdgeRow `json:"runners"`
}

// TestsFor returns configs/keybinds that launch or reload a file. Configs rarely
// have formal tests, so this is best-effort and marked limited.
func (q *Querier) TestsFor(path string) (TestsResult, error) {
	res := TestsResult{
		Limited: true,
		Note:    "no formal tests detected; showing configs/keybinds that launch or reload this file",
	}
	if id, ok, err := q.fileID(path); err == nil && ok {
		res.Runners, _ = q.s.IncomingEdges("file", id, "execs", "binds", "autostarts")
	}
	return res, nil
}

// SimilarCode returns ranked snippets. With an inferencer and a vector index it
// fuses semantic (vector KNN) and lexical (FTS) results via reciprocal rank
// fusion; otherwise it falls back to FTS only.
func (q *Querier) SimilarCode(ctx context.Context, text string) ([]store.ChunkHit, error) {
	if q.inf == nil || !q.s.VectorsReady() {
		return q.searchChunksRanked(text, DefaultLimit)
	}
	return q.hybrid(ctx, text, DefaultLimit)
}

// searchChunksRanked runs the FTS query over a generous pool, then stably
// demotes vendored and generated chunks so a project's own code leads even when
// a dense dependency file (a generated constants table, say) would otherwise
// monopolize the top results by raw FTS rank. The FTS order is kept within each
// tier and the pool is truncated to limit.
func (q *Querier) searchChunksRanked(text string, limit int) ([]store.ChunkHit, error) {
	pool := limit * 4
	if pool < 200 {
		pool = 200
	}
	hits, err := q.s.SearchChunks(text, pool)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return !isVendored(hits[i].File) && isVendored(hits[j].File)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// hybrid embeds the query, runs vector KNN and FTS, and fuses by RRF, falling
// back to FTS alone if embedding fails.
func (q *Querier) hybrid(ctx context.Context, text string, k int) ([]store.ChunkHit, error) {
	fts, err := q.searchChunksRanked(text, k)
	if err != nil {
		return nil, err
	}
	vecs, err := q.inf.Embed(ctx, []string{text})
	if err != nil || len(vecs) == 0 {
		return fts, nil
	}
	vhits, err := q.s.VectorSearch(vecs[0], k)
	if err != nil {
		return fts, nil
	}
	return fuseRRF(vhits, fts, k), nil
}

// fuseRRF merges two ranked lists by reciprocal rank fusion, deduped by file:line.
func fuseRRF(a, b []store.ChunkHit, limit int) []store.ChunkHit {
	const k0 = 60.0
	type agg struct {
		hit   store.ChunkHit
		score float64
	}
	m := map[string]*agg{}
	var order []string
	add := func(list []store.ChunkHit) {
		for rank, h := range list {
			key := h.File + ":" + strconv.Itoa(h.StartLine)
			e, ok := m[key]
			if !ok {
				e = &agg{hit: h}
				m[key] = e
				order = append(order, key)
			}
			e.score += 1.0 / (k0 + float64(rank+1))
		}
	}
	add(a)
	add(b)
	sort.SliceStable(order, func(i, j int) bool { return m[order[i]].score > m[order[j]].score })
	out := make([]store.ChunkHit, 0, limit)
	for _, k := range order {
		if len(out) >= limit {
			break
		}
		out = append(out, m[k].hit)
	}
	return out
}

// SmartResult is the assist-augmented search result.
type SmartResult struct {
	Query     string           `json:"query"`
	Rewritten string           `json:"rewritten,omitempty"`
	Matches   []store.ChunkHit `json:"matches"`
}

// SmartSearch runs the full assist pipeline: an optional query rewrite, hybrid
// vector+FTS retrieval, then a rerank. It falls back to plain FTS when the
// assist layer is unavailable. Every model output is constrained (a query
// string, an index ordering); the model never invents or edits results.
func (q *Querier) SmartSearch(ctx context.Context, text string) (SmartResult, error) {
	res := SmartResult{Query: text}
	if q.inf == nil || !q.s.VectorsReady() {
		hits, err := q.searchChunksRanked(text, DefaultLimit)
		res.Matches = hits
		return res, err
	}
	search := text
	if rw, err := q.inf.Generate(ctx, rewritePrompt(text)); err == nil {
		if c := cleanRewrite(rw); c != "" {
			search, res.Rewritten = c, c
		}
	}
	cand, err := q.hybrid(ctx, search, 20)
	if err != nil {
		return res, err
	}
	if len(cand) > 1 {
		docs := make([]string, len(cand))
		for i, c := range cand {
			docs[i] = c.Snippet
		}
		if order, err := q.inf.Rerank(ctx, text, docs); err == nil && len(order) == len(cand) {
			reordered := make([]store.ChunkHit, 0, len(cand))
			for _, idx := range order {
				reordered = append(reordered, cand[idx])
			}
			cand = reordered
		}
	}
	if len(cand) > DefaultLimit {
		cand = cand[:DefaultLimit]
	}
	res.Matches = cand
	return res, nil
}

func rewritePrompt(q string) string {
	return "Rewrite this into a short keyword search query for a dotfiles/config index. " +
		"Reply with only the keywords, no punctuation or explanation.\nQuery: " + q
}

func cleanRewrite(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) == 0 || len(s) > 120 {
		return ""
	}
	return s
}

// Violation is a deterministic architecture/health finding.
type Violation struct {
	Kind   string `json:"kind"`
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Detail string `json:"detail"`
}

// ArchitectureViolations returns dangling references, orphan scripts, and
// hardcoded colors that duplicate a declared variable.
func (q *Querier) ArchitectureViolations() ([]Violation, error) {
	var v []Violation
	dang, err := q.s.UnresolvedEdges("includes", "references", "uses_resource")
	if err != nil {
		return nil, err
	}
	files, err := q.s.AllFiles()
	if err != nil {
		return nil, err
	}
	langByID := make(map[int64]string, len(files))
	for _, f := range files {
		langByID[f.ID] = f.Lang
	}
	for _, e := range dang {
		if e.Kind == "includes" && store.ModuleImportLang(langByID[e.FileID]) {
			continue // external module import, not a broken project reference
		}
		if e.Kind == "uses_resource" || looksPathy(e.Raw) {
			v = append(v, Violation{Kind: "dangling_" + e.Kind, File: e.File, Line: e.Line, Detail: e.Raw})
		}
	}
	orphans, err := q.s.OrphanFiles("script")
	if err != nil {
		return nil, err
	}
	for _, f := range orphans {
		v = append(v, Violation{Kind: "orphan_script", File: f.RelPath, Detail: "script not referenced by any config or keybind"})
	}
	res, err := q.s.AllResources()
	if err != nil {
		return nil, err
	}
	declByValue := make(map[string]string)
	for _, r := range res {
		if r.Name != "" && r.Value != "" {
			declByValue[r.Value] = r.Name
		}
	}
	for _, r := range res {
		if r.Name == "" && r.Value != "" {
			if name, ok := declByValue[r.Value]; ok {
				v = append(v, Violation{Kind: "hardcoded_color", File: r.File, Line: r.Line, Detail: r.Value + " (use " + name + ")"})
			}
		}
	}
	return v, nil
}

func looksPathy(s string) bool {
	return strings.ContainsAny(s, "/") || strings.HasPrefix(s, "$") || strings.HasPrefix(s, "@")
}

// Hotspots ranks structurally central and large files.
type Hotspots struct {
	FanIn            []store.FanRow   `json:"fan_in"`
	Largest          []FileSize       `json:"largest"`
	LargestFunctions []store.FuncSpan `json:"largest_functions"`
	ComplexFunctions []store.FuncSpan `json:"complex_functions,omitempty"`
}

// FileSize pairs a file with its byte size.
type FileSize struct {
	File string `json:"file"`
	Size int64  `json:"size"`
}

// isVendored reports whether a path is third-party or generated code, which is
// noise in hotspots (you do not refactor your dependencies). Such files stay
// indexed and queryable; they are only dropped from the central/largest/complex
// rankings.
func isVendored(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "vendor", "third_party", "third-party", "node_modules", ".venv", "venv", "site-packages", ".yarn":
			return true
		}
	}
	return strings.HasSuffix(p, ".pb.go") || strings.HasSuffix(p, "_pb2.py") || strings.HasSuffix(p, ".g.dart")
}

// auxiliaryKind reports whether a symbol kind is a config or documentation
// entry (a setting, keybind, color, heading, or config section) rather than a
// code definition. These dominate config-heavy repos and FTS ranks the tiny
// rows high, so find floats real definitions above them without dropping them.
func auxiliaryKind(kind string) bool {
	switch kind {
	case "setting", "keybind", "color", "heading", "config_section":
		return true
	}
	return false
}

// findRank orders FindSymbol results into four tiers: project code definitions,
// then project config/doc entries, then vendored definitions, then vendored
// config/doc. A stable sort keeps the prior order (exact, FTS, substring)
// within each tier, so the highest-precision match still leads.
func findRank(h store.SymbolHit) int {
	r := 0
	if auxiliaryKind(h.Kind) {
		r++
	}
	if isVendored(h.File) {
		r += 2
	}
	return r
}

// RepoHotspots returns fan-in and size rankings over the project's own code
// (vendored and generated files are excluded as noise). Git churn arrives in M3.
func (q *Querier) RepoHotspots() (Hotspots, error) {
	var h Hotspots
	const top, pool = 10, 200
	fan, err := q.s.FanIn(pool)
	if err != nil {
		return h, err
	}
	for _, r := range fan {
		if isVendored(r.File) {
			continue
		}
		if h.FanIn = append(h.FanIn, r); len(h.FanIn) >= top {
			break
		}
	}
	files, err := q.s.AllFiles()
	if err != nil {
		return h, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Size > files[j].Size })
	for _, f := range files {
		if isVendored(f.RelPath) {
			continue
		}
		if h.Largest = append(h.Largest, FileSize{File: f.RelPath, Size: f.Size}); len(h.Largest) >= top {
			break
		}
	}
	lf, _ := q.s.LargestFunctions(pool)
	for _, r := range lf {
		if isVendored(r.File) {
			continue
		}
		if h.LargestFunctions = append(h.LargestFunctions, r); len(h.LargestFunctions) >= top {
			break
		}
	}
	mc, _ := q.s.MostComplex(pool)
	for _, r := range mc {
		if isVendored(r.File) {
			continue
		}
		if h.ComplexFunctions = append(h.ComplexFunctions, r); len(h.ComplexFunctions) >= top {
			break
		}
	}
	return h, nil
}

// Status summarizes index freshness and coverage.
type Status struct {
	Counts    store.Counts `json:"counts"`
	LastIndex string       `json:"last_index"`
	AIEnabled bool         `json:"ai_enabled"`
	Savings   Savings      `json:"savings"`
}

// Savings estimates tokens saved versus reading the files each answer pointed at.
// Tokens are bytes/4 (the usual rough rule). The figure is deliberately
// conservative: it reports only a fraction (savingsMargin) of the measured byte
// difference, so it under-counts rather than over-claims.
type Savings struct {
	Queries      int64 `json:"queries"`
	AnswerTokens int64 `json:"answer_tokens"`
	SavedTokens  int64 `json:"saved_tokens"`
}

// savingsMargin is the fraction of the raw byte difference we report, leaving
// headroom for files an agent would only partly read, result overlap, and
// tokenizer variance.
const savingsMargin = 0.7

// ComputeSavings turns raw usage counters into a conservative savings estimate.
func ComputeSavings(s store.Stats) Savings {
	raw := s.BaselineBytes - s.AnswerBytes
	if raw < 0 {
		raw = 0
	}
	return Savings{
		Queries:      s.Queries,
		AnswerTokens: s.AnswerBytes / 4,
		SavedTokens:  int64(float64(raw) * savingsMargin / 4),
	}
}

// Status returns the index summary.
func (q *Querier) Status() (Status, error) {
	c, err := q.s.Counts()
	if err != nil {
		return Status{}, err
	}
	last, _ := q.s.GetMeta("last_index")
	ai, _ := q.s.GetMeta("ai_enabled")
	stats, _ := q.s.Stats()
	return Status{
		Counts: c, LastIndex: last, AIEnabled: ai == "true",
		Savings: ComputeSavings(stats),
	}, nil
}
