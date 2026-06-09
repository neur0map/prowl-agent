# Changelog

## Unreleased

First working slice: a local-first ricing config-intelligence backend.

### Added

- **CLI**: `init` (interactive setup wizard), `status` (per-project and global
  registry view), and a hidden agent-launched `serve` (MCP stdio server).
- **Indexing pipeline**: ignore-aware walk plus content-hash incremental
  indexing. Only changed files are reparsed; removed files are pruned; graph
  resolution re-runs globally and idempotently.
- **Tree-sitter extraction** for Lua, Python, Bash, CSS, SCSS, JSON, YAML, TOML,
  INI, QML, and Hyprland (`hyprlang`), plus a line-oriented generic extractor for
  bespoke WM configs (sway/i3, rofi `rasi`, polybar, and others).
- **Config graph**: include trees, exec/keybind to script chains, and shared
  resource (color/font/path/variable) references, with best-effort path/name
  resolution and dangling-reference detection.
- **SQLite store**: files/symbols/resources/edges/chunks with FTS5 full-text
  search and a recursive-CTE blast-radius query (WAL mode).
- **15 MCP tools**: `find_symbol`, `find_references`, `find_callers`,
  `find_callees`, `file_relations`, `blast_radius`, `entrypoints_for`,
  `tests_for`, `similar_code`, `smart_search`, `architecture_violations`,
  `repo_hotspots`, `doctor`, `status` (plus `reindex`), served over stdio.
- **Workspace**: per-folder `.prowl/`, global project registry (XDG), automatic
  `.gitignore` wiring, and agent injection (`.mcp.json` and `AGENTS.md`).
- **Semantic search (opt-in)**: a local Ollama `Inferencer` (embed/generate),
  chunk embeddings stored in `sqlite-vec`, and a hybrid `similar_code` that fuses
  vector nearest-neighbor and full-text results (reciprocal rank fusion), with a
  full-text fallback when disabled. The setup wizard detects Ollama and reports
  model setup.
- **Doctor**: `prowl-agent doctor` and a `doctor` MCP tool with deterministic
  checks (cyclic includes, fan-in/out risk, oversized configs, duplicate
  keybinds, broken commands, orphan scripts, dangling references, hardcoded
  colors, rule-driven forbidden crossings, git-churn hotspots) and a 0-100 score.
- **smart_search**: assist-augmented retrieval (query rewrite, hybrid search,
  rerank) with a full-text fallback; a reranker was added to the local Inferencer.
- **Live freshness**: the server watches the rice and re-indexes changed files
  automatically (debounced) while serving.
- **More inject targets**: Cursor (`.cursor/mcp.json`) and VS Code
  (`.vscode/mcp.json`) alongside the generic `.mcp.json`.
- **More languages**: C++ and Fish grammars and extractors.
- **Overview and clusters**: an `overview` tool/map (roles, entrypoints,
  clusters, color palette, keybind count, hotspots) and a `clusters` tool that
  groups files into subsystems via connected components over the graph.
- **Tiered detail**: `detail: compact` on search tools returns file:line only,
  saving tokens; `full` includes snippets.
- **Agent playbook**: the injected `AGENTS.md` now includes a concrete ricing
  workflow (which tools to use for color changes, edits, keybinds, commits).
- **Prebuilt binary**: a CI workflow builds `prowl-agent-linux-amd64` (cgo + FTS5)
  on every push to `main` and publishes it to the rolling `nightly` GitHub release
  with a `.sha256`.

### Notes

- Linux-only; requires cgo and the `sqlite_fts5` build tag.
- `tests_for` is best-effort (rices have no formal tests) and marked `limited`.
