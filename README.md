# Prowl Agent

Local-first ricing / dotfiles config-intelligence backend for coding agents.

Prowl Agent indexes a Linux rice (window-manager configs, bars, widgets, themes,
scripts) into a persistent per-folder SQLite graph plus full-text index, and
exposes precise, relationship-aware queries over the
[Model Context Protocol](https://modelcontextprotocol.io). An agent asks Prowl
Agent for bounded, cited context instead of re-reading the whole rice.

It models the relationships that actually matter in a rice:

- **include trees**: `source=`, `include`, `@import`, `require()`, sway `include`
- **exec / keybind chains**: `exec`/`exec-once`/autostart and `bind = ... exec <script>`
- **shared resources**: colors, fonts, paths, and theme variables across files

## Supported formats

Lua, Python, Bash; CSS, SCSS; TOML, YAML, JSON/JSONC, INI; QML; Hyprland
(`hyprlang`); plus a line-oriented fallback for bespoke WM configs (sway/i3,
rofi `rasi`, polybar, kitty, dunst, and similar).

## Requirements

Linux, Go 1.26+, and a C toolchain (cgo is required for Tree-sitter and SQLite).

## Build

```sh
CGO_ENABLED=1 go build -tags sqlite_fts5 -o prowl-agent ./cmd/prowl-agent
```

The `sqlite_fts5` build tag enables SQLite's FTS5 full-text engine.

## Usage

The command surface is intentionally small: `init`, `status`, `help`.

```sh
# In your dotfiles repo (or ~/.config), set everything up:
prowl-agent init                 # interactive setup wizard
prowl-agent init --no-ai --yes   # non-interactive

# Inspect index state for this project, or list all initialized projects:
prowl-agent status
prowl-agent status --json
```

`init` creates a per-folder `.prowl/` workspace (`config.toml`, `rules.toml`,
`index.db`), runs the first index, registers the MCP server in `.mcp.json`,
writes agent instructions into `AGENTS.md`, and adds `.prowl/` to `.gitignore`.
The index database and other backend state never leave `.prowl/`.

The MCP server is launched by your coding agent through the generated `.mcp.json`
(it runs the hidden `prowl-agent serve` over stdio); you do not run it by hand.

## MCP tools

`find_symbol`, `find_references`, `find_callers`, `find_callees`,
`file_relations`, `blast_radius`, `entrypoints_for`, `tests_for`, `similar_code`,
`architecture_violations`, `repo_hotspots`, `status`, and `reindex`. Every result
is deterministic and carries `file:line` provenance.

## Semantic search (opt-in)

The setup wizard can enable a local semantic layer. It uses Ollama with small,
code-aware models: `qwen3-embedding:0.6b` (embeddings), `qwen3-reranker:0.6b`
(reranking), and `gemma3:4b` (query assist) by default. The assist model is a
retrieval helper only: it never makes decisions, is never exposed as its own
tool, and only rewrites queries, reranks, and compacts results behind
`similar_code`. Structural search works fully without it.

## Architecture

```
cmd/prowl-agent      entry point (cobra)
internal/parse       Tree-sitter grammar loading and per-language extractors
internal/graph       include / exec / resource resolution and role inference
internal/index       ignore-aware walk and hash-based incremental indexing
internal/store       SQLite schema, FTS5, graph reads (blast-radius CTE)
internal/query       the 12 structural query ops
internal/mcp         MCP stdio server
internal/cli         init wizard, status, hidden serve, agent injection
internal/config      config.toml / rules.toml
internal/workspace   .prowl/ workspace, global registry, gitignore wiring
internal/assist      local Inferencer (Ollama) for the semantic layer
```

Indexing is incremental (only changed files are reparsed); graph resolution is
global and idempotent, so the index stays correct as files change.

## Development

Run the test suite:

```sh
CGO_ENABLED=1 go test -tags sqlite_fts5 ./...
```

Commit hooks live in `.githooks/`. Enable them with:

```sh
git config core.hooksPath .githooks
```
