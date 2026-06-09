// Package query implements the 12 structural queries Prowl Agent exposes to
// agents. All results are deterministic and carry file:line provenance.
package query

import (
	"sort"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// Querier answers structural queries against an index.
type Querier struct{ s *store.Store }

// New wraps a store.
func New(s *store.Store) *Querier { return &Querier{s: s} }

// DefaultLimit bounds result sizes.
const DefaultLimit = 50

func (q *Querier) fileID(path string) (int64, bool, error) {
	f, ok, err := q.s.GetFileByPath(path)
	if err != nil || !ok {
		return 0, false, err
	}
	return f.ID, true, nil
}

// FindSymbol returns exact-name matches first, then FTS matches.
func (q *Querier) FindSymbol(name string) ([]store.SymbolHit, error) {
	exact, err := q.s.SymbolsByName(name, DefaultLimit)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool, len(exact))
	out := make([]store.SymbolHit, 0, len(exact))
	for _, h := range exact {
		seen[h.ID] = true
		out = append(out, h)
	}
	if fts, err := q.s.SearchSymbols(name, DefaultLimit); err == nil {
		for _, h := range fts {
			if !seen[h.ID] {
				out = append(out, h)
			}
		}
	}
	return out, nil
}

// FindReferences returns edges pointing at a symbol.
func (q *Querier) FindReferences(symbolID int64) ([]store.EdgeRow, error) {
	return q.s.IncomingEdges("symbol", symbolID)
}

// callerKinds / calleeKinds are the dependency edges used for caller/callee and impact queries.
var depKinds = []string{"includes", "execs", "binds", "autostarts", "references"}

// FindCallers returns configs/scripts that include, exec, or bind to a file.
func (q *Querier) FindCallers(path string) ([]store.EdgeRow, error) {
	id, ok, err := q.fileID(path)
	if err != nil || !ok {
		return nil, err
	}
	return q.s.IncomingEdges("file", id, depKinds...)
}

// FindCallees returns what a file includes, execs, or binds to.
func (q *Querier) FindCallees(path string) ([]store.EdgeRow, error) {
	id, ok, err := q.fileID(path)
	if err != nil || !ok {
		return nil, err
	}
	return q.s.EdgesFromFile(id, depKinds...)
}

// Relations is the neighborhood of a file.
type Relations struct {
	File       string            `json:"file"`
	Exists     bool              `json:"exists"`
	Symbols    []store.SymbolHit `json:"symbols"`
	Includes   []store.EdgeRow   `json:"includes"`
	IncludedBy []store.EdgeRow   `json:"included_by"`
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
	r.Includes, _ = q.s.EdgesFromFile(id, "includes")
	r.IncludedBy, _ = q.s.IncomingEdges("file", id, "includes")
	return r, nil
}

// BlastRadius returns files that transitively depend on a file.
func (q *Querier) BlastRadius(path string) ([]store.Dep, error) {
	id, ok, err := q.fileID(path)
	if err != nil || !ok {
		return nil, err
	}
	return q.s.TransitiveDependents(id)
}

// EntrypointsFor returns the root configs (no incoming dependency edges) from
// which path is reachable.
func (q *Querier) EntrypointsFor(path string) ([]string, error) {
	id, ok, err := q.fileID(path)
	if err != nil || !ok {
		return nil, err
	}
	deps, err := q.s.TransitiveDependents(id)
	if err != nil {
		return nil, err
	}
	if len(deps) == 0 {
		return []string{path}, nil // nothing depends on it -> it is the entrypoint
	}
	var roots []string
	for _, d := range deps {
		did, err := q.s.FileID(d.File)
		if err != nil {
			continue
		}
		in, _ := q.s.IncomingEdges("file", did, depKinds...)
		if len(in) == 0 {
			roots = append(roots, d.File)
		}
	}
	sort.Strings(roots)
	return roots, nil
}

// TestsResult is the (deliberately limited) ricing analogue of tests_for.
type TestsResult struct {
	Limited bool            `json:"limited"`
	Note    string          `json:"note"`
	Runners []store.EdgeRow `json:"runners"`
}

// TestsFor returns configs/keybinds that launch or reload a file. Ricing has no
// formal tests, so this is best-effort and marked limited.
func (q *Querier) TestsFor(path string) (TestsResult, error) {
	res := TestsResult{
		Limited: true,
		Note:    "ricing has no formal tests; showing configs/keybinds that launch or reload this file",
	}
	if id, ok, err := q.fileID(path); err == nil && ok {
		res.Runners, _ = q.s.IncomingEdges("file", id, "execs", "binds", "autostarts")
	}
	return res, nil
}

// SimilarCode returns FTS-ranked snippets (vector search arrives in M2).
func (q *Querier) SimilarCode(text string) ([]store.ChunkHit, error) {
	return q.s.SearchChunks(text, DefaultLimit)
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
	for _, e := range dang {
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
	FanIn   []store.FanRow `json:"fan_in"`
	Largest []FileSize     `json:"largest"`
}

// FileSize pairs a file with its byte size.
type FileSize struct {
	File string `json:"file"`
	Size int64  `json:"size"`
}

// RepoHotspots returns fan-in and size rankings (git churn arrives in M3).
func (q *Querier) RepoHotspots() (Hotspots, error) {
	var h Hotspots
	fan, err := q.s.FanIn(10)
	if err != nil {
		return h, err
	}
	h.FanIn = fan
	files, err := q.s.AllFiles()
	if err != nil {
		return h, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Size > files[j].Size })
	for i, f := range files {
		if i >= 10 {
			break
		}
		h.Largest = append(h.Largest, FileSize{File: f.RelPath, Size: f.Size})
	}
	return h, nil
}

// Status summarizes index freshness and coverage.
type Status struct {
	Counts    store.Counts `json:"counts"`
	LastIndex string       `json:"last_index"`
	AIEnabled bool         `json:"ai_enabled"`
}

// Status returns the index summary.
func (q *Querier) Status() (Status, error) {
	c, err := q.s.Counts()
	if err != nil {
		return Status{}, err
	}
	last, _ := q.s.GetMeta("last_index")
	ai, _ := q.s.GetMeta("ai_enabled")
	return Status{Counts: c, LastIndex: last, AIEnabled: ai == "true"}, nil
}
