# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[semantic versioning](https://semver.org/).

## [Unreleased]

The first working version: a local index that gives AI coding agents fast, cited
answers about a project's files, served over MCP.

### Added

#### Indexing

- Incremental indexing. An ignore-aware walk hashes files and reparses only what
  changed; deleted files are pruned, and graph resolution re-runs each time.
- Tree-sitter extraction for Lua, Python, Bash, Fish, C++, QML, CSS, SCSS, JSON,
  YAML, TOML, INI, and Hyprland, plus a line-based reader for other config formats
  (sway/i3, rofi `rasi`, polybar, and similar).
- A graph of how files connect: include trees, exec and keybind to script chains,
  and shared color/font/path/variable references, with path and name resolution.
  Bare commands resolve against the project's command files by basename.
- SQLite store with FTS5 full-text search and a recursive-CTE blast-radius query,
  in WAL mode so the indexer can write while the server reads.

#### Interface

- 17 MCP tools: `overview`, `clusters`, `find_symbol`, `find_references`,
  `find_callers`, `find_callees`, `file_relations`, `blast_radius`,
  `entrypoints_for`, `tests_for`, `similar_code`, `smart_search`,
  `architecture_violations`, `repo_hotspots`, `doctor`, `status`, and `reindex`.
- CLI: `init` (setup wizard), `status`, and `doctor`, plus a hidden `serve` that
  the coding agent launches over stdio.
- Setup writes `.mcp.json`, `.cursor/mcp.json`, `.vscode/mcp.json`, and an
  `AGENTS.md` block, and keeps state in a gitignored `.prowl/` folder.
- The server watches files and re-indexes on save while it runs.

#### Health checks

- `doctor`, as both a command and an MCP tool, reports cyclic includes,
  fan-in/out risk, oversized files, duplicate keybinds, broken commands, orphan
  scripts, dangling references, hardcoded colors, forbidden layer crossings, and
  git-churn hotspots, with a 0-100 score.
- Tuned to keep false positives low: references are emitted only for path-shaped
  targets, commands resolve against the project, checks skip lifecycle directories
  (migrations, installers, CI, vendor), and only project-relative broken includes
  are flagged. On one 2172-file project this brought the report from 2052 findings
  down to about 90, most of them real.

#### Semantic search (optional)

- A local Ollama layer stores chunk embeddings in sqlite-vec. `similar_code` fuses
  vector and full-text results, and `smart_search` adds a query rewrite and a
  rerank. Both fall back to full-text when the layer is off. The model only
  reorders and rewrites; it never makes decisions and is never its own tool.

#### Build and docs

- CI builds a Linux x86_64 binary (cgo + FTS5) on every push to `main` and
  publishes it, with a checksum, to the rolling `nightly` release.
- README and an architecture write-up in `docs/ARCHITECTURE.md`.

### Notes

- Linux only for now; requires cgo and the `sqlite_fts5` build tag.
- The current focus is dotfiles and configs. Broader language support, including
  web and more scripting languages, is in progress.
- `tests_for` is best-effort and marked `limited`, since configs rarely have
  formal tests.
