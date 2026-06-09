# Architecture

Prowl Agent is a single Go binary. It indexes a project into a per-folder SQLite
database and answers questions over two stdio front ends: MCP for coding agents
and LSP for editors. There is no daemon and no network service; each client
starts the binary itself.

## Packages

```
cmd/prowl-agent      entry point (cobra)
internal/parse       Tree-sitter grammar loading and per-language extractors
internal/graph       include / exec / resource resolution and role inference
internal/index       ignore-aware walk and hash-based incremental indexing
internal/store       SQLite schema, FTS5, sqlite-vec, graph reads (blast-radius CTE)
internal/query       structural queries and hybrid/semantic search
internal/doctor      health checks (cycles, conflicts, hotspots)
internal/mcp         MCP stdio server
internal/lsp         Language Server (stdio) for editors (definition, references, hover, ...)
internal/cli         init wizard, status, doctor, hidden serve, file watcher, injection
internal/config      config.toml / rules.toml
internal/workspace   .prowl/ workspace, global registry, gitignore wiring
internal/assist      local Ollama inferencer for the semantic layer
```

## How it works

1. **Walk and parse.** `index` walks the project, skipping ignored paths. Each
   file is parsed by the matching Tree-sitter grammar (or a line-based reader for
   config formats without a grammar) into symbols, resources, and raw edges.
2. **Resolve the graph.** `graph` turns raw edges into real links: include trees,
   exec and keybind to script chains, and shared color/font/path/variable
   references. Bare commands resolve against the project's command files by
   basename. Each file gets a role (config, bar, theme, script, and so on).
3. **Store.** Everything lands in SQLite with an FTS5 full-text index and, when the
   semantic layer is on, chunk embeddings in sqlite-vec. Blast-radius uses a
   recursive CTE.
4. **Serve.** `mcp` exposes the queries to coding agents as tools; `lsp` exposes
   the same index to editors as a language server (definition, references, hover,
   document/workspace symbols, code lens, completion, and `doctor` diagnostics).
   Both carry `file:line` provenance and share the one `.prowl/index.db`.

Indexing is incremental: only files whose content hash changed are reparsed, and
graph resolution re-runs globally so the index stays correct as files move around.

## Semantic layer

When enabled, `assist` talks to a local Ollama instance. Embeddings power
`similar_code` (vector nearest-neighbor fused with full-text search by reciprocal
rank fusion), and a small helper model can rewrite and re-rank queries for
`smart_search`. The helper only reorders and rewrites; it never invents results
and is never exposed as its own tool. The embed model warms once at startup and
stays resident for a keep-alive window, so queries are hot after the first.

## Development

Run the test suite (cgo and the FTS5 tag are required):

```sh
CGO_ENABLED=1 go test -tags sqlite_fts5 ./...
```

Commit hooks live in `.githooks/`. Enable them with:

```sh
git config core.hooksPath .githooks
```
