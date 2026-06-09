// Package mcp exposes Prowl Agent's structural queries to coding agents as MCP
// tools over stdio, using the official MCP Go SDK.
package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prowl-agent/prowl-agent/internal/doctor"
	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

// ReindexFunc refreshes the index and returns a human-readable summary.
type ReindexFunc func(ctx context.Context) (string, error)

// DoctorFunc runs health diagnostics for the project.
type DoctorFunc func(ctx context.Context) (doctor.Report, error)

type handlers struct {
	q       *query.Querier
	reindex ReindexFunc
	doctor  DoctorFunc
}

// Empty is the input type for tools that take no arguments.
type Empty struct{}

// NewServer builds an MCP server exposing the query tools, reindex, and doctor.
func NewServer(q *query.Querier, version string, reindex ReindexFunc, doctorFn DoctorFunc) *sdk.Server {
	h := &handlers{q: q, reindex: reindex, doctor: doctorFn}
	s := sdk.NewServer(&sdk.Implementation{Name: "prowl-agent", Version: version}, nil)

	sdk.AddTool(s, &sdk.Tool{Name: "find_symbol",
		Description: "Find symbols (functions, settings, keybinds, components, ids) by name."}, h.findSymbol)
	sdk.AddTool(s, &sdk.Tool{Name: "find_references",
		Description: "Find edges pointing at a symbol id."}, h.findReferences)
	sdk.AddTool(s, &sdk.Tool{Name: "find_callers",
		Description: "Configs/scripts that include, exec, or bind to a file."}, h.findCallers)
	sdk.AddTool(s, &sdk.Tool{Name: "find_callees",
		Description: "What a file includes, execs, or binds to."}, h.findCallees)
	sdk.AddTool(s, &sdk.Tool{Name: "file_relations",
		Description: "A file's defined symbols and include neighbors."}, h.fileRelations)
	sdk.AddTool(s, &sdk.Tool{Name: "blast_radius",
		Description: "Files that transitively depend on a file (change impact)."}, h.blastRadius)
	sdk.AddTool(s, &sdk.Tool{Name: "entrypoints_for",
		Description: "Root files from which a file is reachable (its entry points)."}, h.entrypointsFor)
	sdk.AddTool(s, &sdk.Tool{Name: "tests_for",
		Description: "Configs/keybinds that launch or reload a file (best-effort)."}, h.testsFor)
	sdk.AddTool(s, &sdk.Tool{Name: "similar_code",
		Description: "Search file content. Hybrid vector and full-text when the semantic layer is enabled, else full-text. Returns cited snippets."}, h.similarCode)
	sdk.AddTool(s, &sdk.Tool{Name: "smart_search",
		Description: "Assist-augmented search: rewrites the query, runs hybrid retrieval, and reranks. Best for fuzzy/natural-language queries. Falls back to full-text when the semantic layer is off."}, h.smartSearch)
	sdk.AddTool(s, &sdk.Tool{Name: "architecture_violations",
		Description: "Dangling references, orphan scripts, and hardcoded colors."}, h.architectureViolations)
	sdk.AddTool(s, &sdk.Tool{Name: "repo_hotspots",
		Description: "Structurally central and large files."}, h.repoHotspots)
	sdk.AddTool(s, &sdk.Tool{Name: "status",
		Description: "Index freshness, counts, languages, and AI status."}, h.status)
	sdk.AddTool(s, &sdk.Tool{Name: "overview",
		Description: "High-level map of the project: role breakdown, entrypoints, clusters, color palette, keybind count, languages, and hotspots. A good first call on a new project."}, h.overview)
	sdk.AddTool(s, &sdk.Tool{Name: "clusters",
		Description: "Group related files into subsystems (connected via includes, exec chains, and shared resources)."}, h.clusters)
	sdk.AddTool(s, &sdk.Tool{Name: "reindex",
		Description: "Re-scan the project and refresh the index incrementally."}, h.reindexTool)
	sdk.AddTool(s, &sdk.Tool{Name: "doctor",
		Description: "Health diagnostics: cyclic includes, fan-in/out risk, oversized files, duplicate keybinds, broken commands, orphan scripts, dangling references, hardcoded colors, forbidden crossings, churn hotspots. Returns findings and a 0-100 score."}, h.doctorTool)
	return s
}

// Serve runs the server over stdio until the client disconnects.
func Serve(ctx context.Context, s *sdk.Server) error {
	return s.Run(ctx, &sdk.StdioTransport{})
}

// --- inputs ---

type nameIn struct {
	Name string `json:"name" jsonschema:"symbol name to find"`
}
type symbolIn struct {
	SymbolID int64 `json:"symbol_id" jsonschema:"symbol id from find_symbol"`
}
type pathIn struct {
	Path string `json:"path" jsonschema:"file path relative to the project root"`
}
type queryIn struct {
	Query  string `json:"query" jsonschema:"free-text search query"`
	Detail string `json:"detail,omitempty" jsonschema:"result detail: 'compact' for file:line only, 'full' for snippets (default full)"`
}

// --- outputs ---

type symbolsOut struct {
	Symbols []store.SymbolHit `json:"symbols"`
}
type edgesOut struct {
	Edges []store.EdgeRow `json:"edges"`
}
type entrypointsOut struct {
	Entrypoints []string `json:"entrypoints"`
}
type chunksOut struct {
	Matches []store.ChunkHit `json:"matches"`
}
type violationsOut struct {
	Violations []query.Violation `json:"violations"`
}
type messageOut struct {
	Message string `json:"message"`
}

// --- handlers ---

func (h *handlers) findSymbol(ctx context.Context, _ *sdk.CallToolRequest, in nameIn) (*sdk.CallToolResult, symbolsOut, error) {
	hits, err := h.q.FindSymbol(in.Name)
	return nil, symbolsOut{Symbols: hits}, err
}

func (h *handlers) findReferences(ctx context.Context, _ *sdk.CallToolRequest, in symbolIn) (*sdk.CallToolResult, edgesOut, error) {
	e, err := h.q.FindReferences(in.SymbolID)
	return nil, edgesOut{Edges: e}, err
}

func (h *handlers) findCallers(ctx context.Context, _ *sdk.CallToolRequest, in pathIn) (*sdk.CallToolResult, edgesOut, error) {
	e, err := h.q.FindCallers(in.Path)
	return nil, edgesOut{Edges: e}, err
}

func (h *handlers) findCallees(ctx context.Context, _ *sdk.CallToolRequest, in pathIn) (*sdk.CallToolResult, edgesOut, error) {
	e, err := h.q.FindCallees(in.Path)
	return nil, edgesOut{Edges: e}, err
}

func (h *handlers) fileRelations(ctx context.Context, _ *sdk.CallToolRequest, in pathIn) (*sdk.CallToolResult, query.Relations, error) {
	r, err := h.q.FileRelations(in.Path)
	return nil, r, err
}

func (h *handlers) blastRadius(ctx context.Context, _ *sdk.CallToolRequest, in pathIn) (*sdk.CallToolResult, struct {
	Impacted []store.Dep `json:"impacted"`
}, error) {
	d, err := h.q.BlastRadius(in.Path)
	return nil, struct {
		Impacted []store.Dep `json:"impacted"`
	}{Impacted: d}, err
}

func (h *handlers) entrypointsFor(ctx context.Context, _ *sdk.CallToolRequest, in pathIn) (*sdk.CallToolResult, entrypointsOut, error) {
	e, err := h.q.EntrypointsFor(in.Path)
	return nil, entrypointsOut{Entrypoints: e}, err
}

func (h *handlers) testsFor(ctx context.Context, _ *sdk.CallToolRequest, in pathIn) (*sdk.CallToolResult, query.TestsResult, error) {
	r, err := h.q.TestsFor(in.Path)
	return nil, r, err
}

func (h *handlers) similarCode(ctx context.Context, _ *sdk.CallToolRequest, in queryIn) (*sdk.CallToolResult, chunksOut, error) {
	m, err := h.q.SimilarCode(ctx, in.Query)
	return nil, chunksOut{Matches: compactIf(in.Detail, m)}, err
}

func (h *handlers) smartSearch(ctx context.Context, _ *sdk.CallToolRequest, in queryIn) (*sdk.CallToolResult, query.SmartResult, error) {
	r, err := h.q.SmartSearch(ctx, in.Query)
	r.Matches = compactIf(in.Detail, r.Matches)
	return nil, r, err
}

func (h *handlers) overview(ctx context.Context, _ *sdk.CallToolRequest, _ Empty) (*sdk.CallToolResult, query.Overview, error) {
	o, err := h.q.Overview()
	return nil, o, err
}

type clustersOut struct {
	Clusters []query.Cluster `json:"clusters"`
}

func (h *handlers) clusters(ctx context.Context, _ *sdk.CallToolRequest, _ Empty) (*sdk.CallToolResult, clustersOut, error) {
	c, err := h.q.Clusters()
	return nil, clustersOut{Clusters: c}, err
}

// compactIf strips snippets when detail == "compact", for token-lean results.
func compactIf(detail string, hits []store.ChunkHit) []store.ChunkHit {
	if detail != "compact" {
		return hits
	}
	out := make([]store.ChunkHit, len(hits))
	for i, h := range hits {
		h.Snippet = ""
		out[i] = h
	}
	return out
}

func (h *handlers) architectureViolations(ctx context.Context, _ *sdk.CallToolRequest, _ Empty) (*sdk.CallToolResult, violationsOut, error) {
	v, err := h.q.ArchitectureViolations()
	return nil, violationsOut{Violations: v}, err
}

func (h *handlers) repoHotspots(ctx context.Context, _ *sdk.CallToolRequest, _ Empty) (*sdk.CallToolResult, query.Hotspots, error) {
	hs, err := h.q.RepoHotspots()
	return nil, hs, err
}

func (h *handlers) status(ctx context.Context, _ *sdk.CallToolRequest, _ Empty) (*sdk.CallToolResult, query.Status, error) {
	st, err := h.q.Status()
	return nil, st, err
}

func (h *handlers) reindexTool(ctx context.Context, _ *sdk.CallToolRequest, _ Empty) (*sdk.CallToolResult, messageOut, error) {
	if h.reindex == nil {
		return nil, messageOut{Message: "reindex not available"}, nil
	}
	msg, err := h.reindex(ctx)
	return nil, messageOut{Message: msg}, err
}

func (h *handlers) doctorTool(ctx context.Context, _ *sdk.CallToolRequest, _ Empty) (*sdk.CallToolResult, doctor.Report, error) {
	if h.doctor == nil {
		return nil, doctor.Report{Summary: map[string]int{}}, nil
	}
	r, err := h.doctor(ctx)
	return nil, r, err
}
