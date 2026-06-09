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
- **12 MCP tools**: `find_symbol`, `find_references`, `find_callers`,
  `find_callees`, `file_relations`, `blast_radius`, `entrypoints_for`,
  `tests_for`, `similar_code`, `architecture_violations`, `repo_hotspots`,
  `status` (plus `reindex`), served over stdio.
- **Workspace**: per-folder `.prowl/`, global project registry (XDG), automatic
  `.gitignore` wiring, and agent injection (`.mcp.json` and `AGENTS.md`).
- **Semantic layer seams**: local Ollama `Inferencer` (embed/generate) and
  configuration; the setup wizard detects Ollama and reports model setup.

### Notes

- Linux-only; requires cgo and the `sqlite_fts5` build tag.
- `tests_for` is best-effort (rices have no formal tests) and marked `limited`.
