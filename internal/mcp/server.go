// Package mcp exposes Prowl Agent's structural queries to coding agents as MCP
// tools over stdio, using the official MCP Go SDK.
package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

// ReindexFunc refreshes the index and returns a human-readable summary.
type ReindexFunc func(ctx context.Context) (string, error)

type handlers struct {
	q       *query.Querier
	reindex ReindexFunc
}

// Empty is the input type for tools that take no arguments.
type Empty struct{}

// NewServer builds an MCP server exposing the 12 query tools plus reindex.
func NewServer(q *query.Querier, version string, reindex ReindexFunc) *sdk.Server {
	h := &handlers{q: q, reindex: reindex}
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
		Description: "Root configs from which a file is reachable (session/WM entry)."}, h.entrypointsFor)
	sdk.AddTool(s, &sdk.Tool{Name: "tests_for",
		Description: "Configs/keybinds that launch or reload a file (best-effort for ricing)."}, h.testsFor)
	sdk.AddTool(s, &sdk.Tool{Name: "similar_code",
		Description: "Full-text search over config/script content (semantic search arrives in M2)."}, h.similarCode)
	sdk.AddTool(s, &sdk.Tool{Name: "architecture_violations",
		Description: "Dangling references, orphan scripts, and hardcoded colors."}, h.architectureViolations)
	sdk.AddTool(s, &sdk.Tool{Name: "repo_hotspots",
		Description: "Structurally central and large files."}, h.repoHotspots)
	sdk.AddTool(s, &sdk.Tool{Name: "status",
		Description: "Index freshness, counts, languages, and AI status."}, h.status)
	sdk.AddTool(s, &sdk.Tool{Name: "reindex",
		Description: "Re-scan the rice and refresh the index incrementally."}, h.reindexTool)
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
	Path string `json:"path" jsonschema:"file path relative to the rice root"`
}
type queryIn struct {
	Query string `json:"query" jsonschema:"free-text search query"`
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
	m, err := h.q.SimilarCode(in.Query)
	return nil, chunksOut{Matches: m}, err
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
