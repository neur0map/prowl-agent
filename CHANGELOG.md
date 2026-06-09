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
- CLI: `init` (setup wizard), `status`, and `doctor`, plus hidden `serve` (MCP)
  and `lsp` (editor language server) commands launched over stdio.
- Setup writes `.mcp.json`, `.cursor/mcp.json`, `.vscode/mcp.json`, and an
  `AGENTS.md` block, and keeps state in a gitignored `.prowl/` folder.
- The server watches files and re-indexes on save while it runs.

#### Editor (LSP)

- A language server (`prowl-agent lsp`) serves the same index to editors:
  go-to-definition (keybind to script, include to file, variable to declaration),
  find-references, hover with use counts, document and workspace symbols, code
  lens, completion of known variables and colors, and inline `doctor` diagnostics.
- `init` wires it up: editor configs under `.prowl/editor/`, plus a project-local
  `.helix/languages.toml` when there is none to overwrite; Neovim attaches it
  additively alongside your other servers.

#### Install, status, and updates

- One-line installer (`install.sh`) that downloads, checksum-verifies, and drops
  the binary in `~/.local/bin`.
- `prowl-agent update` replaces the running binary with the latest published build
  (downloaded and checksum-verified). `prowl-agent status` reports when a new build
  is out via a cached, anonymous checksum check (skipped for dev builds).
- Redesigned `prowl-agent status`: a bordered card (in a terminal) with index
  stats, a language breakdown, and a token-savings report. Savings are measured per
  answer (the bytes each answer returned versus the size of the files it pointed
  at), so the figure is grounded rather than a flat multiplier. Plain text when
  piped; `--json` carries the numbers.

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
- Works in any repo, not just `~/.config`. It indexes what git tracks and keeps
  its index in a gitignored `.prowl/`; agents read your real files, never
  `.prowl/`, so the gitignore does not hide code from them.
- `tests_for` is best-effort and marked `limited`, since configs rarely have
  formal tests.
