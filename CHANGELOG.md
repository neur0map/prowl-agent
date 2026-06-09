# Changelog

## [Unreleased] — M1: Indexed Core

First working slice: a local-first ricing config-intelligence backend.

### Added

- **CLI:** `init` (interactive setup wizard), `status` (per-project + global
  registry view), and a hidden agent-launched `serve` (MCP stdio server).
- **Indexing pipeline:** ignore-aware walk + content-hash incremental indexing.
  Only changed files are reparsed; removed files are pruned; graph resolution
  re-runs globally and idempotently.
- **Tree-sitter extraction** for Lua, Python, Bash, CSS, SCSS, JSON, YAML, TOML,
  INI, QML, and Hyprland (`hyprlang`), plus a line-oriented generic extractor for
  bespoke WM configs (sway/i3, rofi `rasi`, polybar, …).
- **Ricing graph:** include trees, exec/keybind→script chains, and shared-resource
  (color/font/path/variable) references, with best-effort path/name resolution and
  dangling-reference detection.
- **SQLite store:** files/symbols/resources/edges/chunks with FTS5 full-text
  search and a recursive-CTE blast-radius query (WAL mode).
- **12 MCP tools:** `find_symbol`, `find_references`, `find_callers`,
  `find_callees`, `file_relations`, `blast_radius`, `entrypoints_for`,
  `tests_for`, `similar_code`, `architecture_violations`, `repo_hotspots`,
  `status` (+ `reindex`), via the official MCP Go SDK over stdio.
- **Workspace:** per-folder `.prowl/`, global project registry (XDG), automatic
  `.gitignore` wiring, and agent injection (`.mcp.json` + `AGENTS.md`).
- **AI-assist seams:** Ollama `Inferencer` (embed/generate) + configuration; the
  init wizard detects Ollama and reports model setup. Semantic wiring is deferred
  to M2.

### Notes

- Linux-only; requires cgo and the `sqlite_fts5` build tag.
- `similar_code` is FTS-backed in M1; vector search arrives in M2.
- `tests_for` is best-effort (ricing has no formal tests) and marked `limited`.
