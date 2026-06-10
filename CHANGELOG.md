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
- A binary upgrade forces a full re-parse: the index records the binary's version,
  so extractor and resolver fixes apply on update instead of incremental hashing
  skipping unchanged files and serving stale data. Release builds key this off the
  commit; dev and dirty builds key off the binary's mtime so each local rebuild
  also reparses.
- Tree-sitter extraction for Lua, Python, JavaScript, Bash, Fish, C++, QML, CSS,
  SCSS, Markdown, JSON, YAML, TOML, INI, and Hyprland, plus a line-based reader for
  other config formats (sway/i3, rofi `rasi`, polybar, and similar). Markdown
  headings and JavaScript declarations become symbols, so docs and dashboard
  scripts are searchable by name as well as by content.
- A graph of how files connect: include trees, exec and keybind to script chains,
  and shared color/font/path/variable references, with path and name resolution.
  Bare commands resolve against the project's command files by basename.
- QML graph resolution: component instantiations (`Foo { }`) resolve to the
  defining `Foo.qml` (same directory, then repo-unique stem, then nearest path),
  so the QML UI now forms its own subsystems instead of being invisible. Built-in
  and external types (QtQuick/Quickshell) are dropped rather than left dangling.
  `clusters` now reports each subsystem's dominant language. On a QML-heavy rice
  this took resolved edges from 228 to ~1950 and surfaced the 498-file QML shell
  as the top cluster (previously zero QML clusters).
- SQLite store with FTS5 full-text search and a recursive-CTE blast-radius query,
  in WAL mode so the indexer can write while the server reads.

#### Interface

- 17 MCP tools: `overview`, `clusters`, `find_symbol`, `find_references`,
  `find_callers`, `find_callees`, `file_relations`, `blast_radius`,
  `entrypoints_for`, `tests_for`, `similar_code`, `smart_search`,
  `architecture_violations`, `repo_hotspots`, `doctor`, `status`, and `reindex`.
- CLI: `init` (setup wizard), `status`, `doctor`, and `restart`, plus hidden
  `serve` (MCP) and `lsp` (editor language server) commands launched over stdio.
  `restart` rebuilds the structural index from scratch and stops running serve/lsp
  processes so the agent or editor relaunches the current binary; the relaunched
  server re-embeds lazily, so an Ollama or model issue cannot block the restart.
- Setup writes MCP configs: the standard `.mcp.json` (most agents), Cursor, VS Code, Oh My
  Pi (`.omp/mcp.json`), Factory droid (`.factory/mcp.json`), and OpenCode
  (`opencode.json`, its own shape), plus an `AGENTS.md` block; state stays in a
  gitignored `.prowl/` folder. Server entries now include `type: "stdio"` (which
  VS Code requires). The README documents the one-command setup for any other agent.
- Automatic freshness (no daemon, no extra command): the MCP server re-indexes
  right before a request when a change is pending, keeps a featherweight fsnotify
  watcher active for 30 minutes after each call, then suspends and resumes (with a
  catch-up re-index) on the next call, so agents never read stale data. `lsp` also
  re-indexes on save while it runs.

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
  (downloaded and checksum-verified). `prowl-agent status` shows update status
  ("up to date" or "update available") by comparing the build's commit against the
  latest commit on main (cached briefly, recomputed against the running build so
  it is correct right after an update); it works for source builds too via the
  embedded VCS revision.
- Redesigned `prowl-agent status`: a bordered card (in a terminal) with index
  stats, a language breakdown, and a token-savings report. Savings are measured
  per answer (the bytes each answer returned versus the size of the files it
  pointed at), kept at a conservative ~70% so the figure under-counts rather than
  over-claims. It aggregates across every initialized project for a combined total
  and links to `docs/TOKENS.md` so users can reproduce the measurement. Plain text
  when piped; `--json` carries the numbers.

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
- Resilience: if the configured embed model is missing or Ollama is unreachable,
  the server logs a notice and runs structural-only instead of failing to start,
  and an embedding error during a refresh never fails the index.
- The `init` wizard offers model tiers (`fast` / `smart` / `max`, or `--tier`),
  but prefers models already installed on Ollama over the tier preset, so it never
  points the config at an absent model or asks you to pull a redundant one when a
  usable embedder (for example `nomic-embed-text`) is already present. It offers to
  run the official Ollama installer and pulls only genuinely missing models. Tier
  presets track current best local models: `qwen3-embedding` for retrieval,
  `embeddinggemma` on the fast tier, `gemma4:e2b`/`e4b` for the smart/max assist,
  and `gemma3:1b` for the fast assist.

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
