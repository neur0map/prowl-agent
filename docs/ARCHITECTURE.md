# Architecture

Prowl Agent is a single Go binary. It indexes a project into a per-folder SQLite
database and answers questions three ways from one index: read-only shell
commands (the primary path coding agents use), an MCP stdio server, and an LSP
stdio server for editors. There is no daemon and no network service; each query
runs the binary, and MCP/LSP clients start it themselves.

Shell commands are the recommended path: an agent runs `prowl-agent find foo`
(or `overview`, `impact`, `changed`, ...) and gets a cited, token-lean answer
with no server to start and none of MCP's upfront per-call tool-schema cost.
Output defaults to TOON (Token-Oriented Object Notation): uniform result arrays
collapse to one header plus CSV-style rows, which models read more cheaply than
JSON. `--json` switches any command to JSON, the shape MCP also returns.

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
internal/cli         commands: init (setup + optional Ollama lifecycle), the read-only query commands (find, search, overview, impact, changed, hotspots, ...), status, doctor, restart, update, version, hidden serve/lsp, file watcher, injection, TOON/JSON formatting
internal/config      per-project config.toml / rules.toml and a global ~/.config/prowl-agent/config.toml that remembers the semantic tier and backend
internal/workspace   .prowl/ workspace, global registry, gitignore wiring
internal/embed       in-process static embedder: bundled model2vec model + WordPiece tokenizer (semantic search, no setup)
internal/assist      optional Ollama / coding-agent inferencer (higher-quality embeddings, query rewrite, rerank)
```

## How it works

1. **Walk and parse.** `index` walks the project, skipping ignored paths. Each
   file is parsed by the matching Tree-sitter grammar (or a line-based reader for
   config formats without a grammar) into symbols, resources, and raw edges.
2. **Resolve the graph.** `graph` turns raw edges into real links: include trees,
   exec and keybind to script chains, and shared color/font/path/variable
   references. Bare commands resolve against the project's command files by
   basename. Each file gets a role (config, bar, theme, script, and so on).
   For code languages, Go resolves in-module package imports to every file of the
   imported package (read from `go.mod`), TypeScript/JavaScript resolve relative
   imports, first-party monorepo packages (a `@scope/pkg` / `pkg/subpath` import
   resolves to that package's source by the `src/` convention, mapping the
   `package.json` name to its directory; built `dist/` paths from `exports` are
   ignored so edges land on real source), and tsconfig path aliases (`@/x` ->
   `src/x`, read from each tsconfig.json/jsconfig.json `paths` and scoped to that
   config's directory), Rust resolves `mod` declarations and
   `crate::` imports to module files (single crate or Cargo workspace), Python
   resolves absolute imports, and PHP resolves a `use Ns\Class` import to the
   file that declares that class (matching the class's recorded namespace and
   basename, so a namespace that lives in an off-convention directory still
   resolves), Java and Kotlin resolve an `import` to the class file under any
   module's source root (Maven, Gradle, or Kotlin-Multiplatform layouts; Kotlin
   and Java resolve to each other, and a member or nested-type import folds to
   its enclosing class file), Dart resolves a `package:<name>/path` or
   relative import to a workspace package's `lib/` (the name read from each
   pubspec.yaml), and Elixir resolves an `alias`/`import`/`use` to the file
   declaring that module (the `defmodule` name, like C# namespaces), so the graph
   queries work across a Go module, a TS app or monorepo, a Rust crate, a Python
   package, a PHP project, a JVM project, a Dart/Flutter app, or an Elixir/Phoenix
   project. External and standard-library imports stay informational.
3. **Store.** Everything lands in SQLite with an FTS5 full-text index and chunk
   embeddings in sqlite-vec. Blast-radius loads the
   resolved edge set once and walks it with an in-memory BFS.
4. **Answer.** The shell query commands (in `cli`) run a querier directly and
   print TOON or JSON; `mcp` exposes the same queries to coding agents as tools;
   `lsp` exposes the index to editors (definition, references, hover,
   document/workspace symbols, code lens, completion, and `doctor` diagnostics).
   All three carry `file:line` provenance and share the one `.prowl/index.db`.
   Each shell query and the MCP server freshen the index incrementally first, so
   answers are never stale; both also record the token savings behind
   `prowl-agent status`.

Indexing is incremental: only files whose content hash changed are reparsed, and
graph resolution re-runs globally so the index stays correct as files move around.

## Semantic layer

Semantic search is always on and needs no setup. `embed` runs a small,
code-trained static model (model2vec `potion-code-16M`) that ships inside the
binary: it tokenizes with WordPiece, looks up a vector per token, takes a
weighted mean, and normalizes -- no neural runtime, no daemon, no download. Those
vectors live in `sqlite-vec` and power `similar_code` (vector nearest-neighbor
fused with full-text search by reciprocal rank fusion). `smart_search` adds a
query rewrite and a re-rank on top.

`assist` is the optional upgrade. If a local Ollama embed model is present it is
used instead (higher-quality embeddings, all-in-one embed/rewrite/rerank), and
`init` can manage the Ollama lifecycle (reuse a service, install a user
`ollama.service`, or spawn it, then warm the model). Failing that, a detected
coding-agent CLI supplies the rewrite and re-rank step. If neither is present the
built-in embedder still gives you vector search; only a total embedder failure
falls back to plain full-text search. Any helper model only reorders or rewrites
-- it never invents results and is never exposed as its own tool.

## Development

Run the test suite (cgo and the FTS5 tag are required):

```sh
CGO_ENABLED=1 go test -tags sqlite_fts5 ./...
```

Commit hooks live in `.githooks/`. Enable them with:

```sh
git config core.hooksPath .githooks
```
