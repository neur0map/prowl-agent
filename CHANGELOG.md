# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[semantic versioning](https://semver.org/).

## [Unreleased]

### Changed

- `references <id>` for a function or method now matches call syntax (the name
  invoked with `(`), not every bare mention, so a signature-change blast radius
  excludes config/struct fields and identifiers that merely share the name. On
  lazygit, `references` for the `CopyToClipboard` method went from 40 noisy hits
  (22 of them non-calls that also crowded real calls out of the result cap) to
  the exact 23 call sites across 8 files -- matching a careful hand-grep.
- `search <text>` ranks a project's implementation above tests, then docs and
  i18n/locale string tables, then vendored/generated code. Lexical search
  otherwise floats the files that carry human-readable feature wording (string
  tables, translated docs, integration tests) above the code that implements it:
  a concept query like "stage or unstage a line or hunk" on lazygit buried the
  real patch-handling source at ranks 9-14 under i18n/docs/tests; it now leads.
  Demotion only -- nothing is dropped.

## [0.8.0] - 2026-06-21

### Added

- Elixir is a first-class language. A Tree-sitter extractor records modules
  (`defmodule`), functions (`def`/`defp`/`defmacro`), and `alias`/`import`/`use`
  edges; each module name is recorded like a namespace, so an `alias MyApp.Foo`
  resolves to the file declaring `defmodule MyApp.Foo` (the C# namespace model,
  generalized). Validated on Phoenix: 297 module dependencies resolved across
  287 files, so `impact`, `callers`, `clusters`, and `references` work on an
  Elixir/Phoenix project. The `alias A.{B, C}` group form is expanded.

### Changed

- `find <name>` ranks its results so a project's own code definitions come
  before config/doc entries (settings, headings) and before vendored or
  generated files, keeping the match-quality order within each tier. On a
  config-heavy repo a query like `find run` previously buried the actual `run`
  methods under dozens of YAML `run:` keys; the definitions now lead. No results
  are dropped, so config-only lookups are unaffected.
- `references <id>` tags each code call site with its enclosing function or type
  (an `in` column), so the answer reads as which functions call a symbol
  (`NewGui` is called from `NewApp`, `initGocui`, `RunTUI`, ...) instead of a
  bare file:line list. Computed from indexed line ranges; the column is empty
  for usages at file scope (comments, top-level code). Lines that are themselves
  a same-named definition (an interface method, an override in another class)
  are dropped, so an overridden method's results show real calls, not siblings.
- `search <text>` demotes vendored and generated files so a project's own code
  leads. A dense dependency file (a generated constants table, say) used to
  monopolize the results by raw FTS rank -- `search status` on a Go repo
  returned 50 hits all from a vendored Windows-errors file and none of the
  project's status code; the query now pulls from a larger pool and floats
  project chunks first, keeping FTS order within each tier.
- `search <text>` falls back from an exact-phrase match to all-terms (AND) and
  then any-term (OR) only when the phrase matches nothing, so a non-AI,
  natural-language query returns the most relevant chunks instead of an empty
  result. `search "render the status panel"` (no verbatim phrase) now surfaces
  the files that draw and render the panel; an exact phrase still matches first.

## [0.7.0] - 2026-06-21

Language breadth, symbol signatures, and token-economy hardening on large
real-world repositories, all on top of the 0.6.0 index.

### Added

- PHP, Kotlin, and Dart are first-class languages now. Each has a Tree-sitter
  extractor (classes/interfaces/traits/enums/objects/mixins/extensions,
  functions and methods with cyclomatic complexity) and import-graph resolution:
  PHP `use Ns\Class` resolves to the file declaring that class (matched on the
  recorded namespace plus basename); Kotlin shares a JVM resolver with Java so
  the two resolve to each other across Maven/Gradle/Kotlin-Multiplatform source
  roots, folding a member or nested-type import to its enclosing class file;
  Dart resolves `package:<name>/...` to a workspace package's `lib/` (from each
  pubspec.yaml) and relative/part imports as paths. Validated on Laravel (8237
  `use` imports), OkHttp (1944, cross-language), and LocalSend (977 `package:`).
- Symbol signatures: every code language records a function/method/type's
  declaration header (name, parameters, return type, a class's extends/
  implements), so `find` and `search` hand the agent a symbol's interface inline
  instead of only a location. Signatures are full-text indexed too.
- TypeScript/JavaScript tsconfig path aliases: an import matching a `paths`
  wildcard (`@/components/Button` with `"paths": {"@/*": ["src/*"]}`) resolves to
  the real source, scoped to the nearest tsconfig and following a local
  `extends` to a shared base config. Validated on shadcn/ui (8382 `@/` imports).

### Changed

- `references <id>` returns precise `{file, line, text}` call sites for code
  symbols (the exact usage line, matched on an identifier boundary) instead of
  40-line full-text chunks.
- `overview`, `impact`, and `entrypoints <file>` cap large lists by default: an
  exact count plus a shallow-first sample, so a hub file or large codebase keeps
  the first-call answer token-lean (Laravel overview ~59 KB to ~2.5 KB, impact
  on a 1527-dependent file ~12 KB to ~1.2 KB). `impact --all` still lists every
  dependent.
- The generated `AGENTS.md` guidance steers agents to read a symbol's signature
  from `find` and cited call sites from `references` before opening files.

### Fixed

- Nested `.gitignore` files are honored, each scoped to its own directory
  (deeper rules win), so a monorepo's per-package `dist/`/`build/` ignores keep
  generated output out of the index.
- PHP/Kotlin/Dart external imports (`package:flutter/...`, `Symfony\...`) are no
  longer flagged as `dangling_includes` violations (Laravel `violations` ~106 KB
  to ~4.6 KB).
- A Dart `part of` directive no longer emits a reverse edge that faked a cycle
  with a generated companion file (`.g.dart`, `.freezed.dart`).
- The `AGENTS.md` block is refreshed in place by re-init without touching the
  user's own content, even when the closing marker is missing.

## [0.6.0] - 2026-06-21

The first working version: a local index that gives AI coding agents fast, cited
answers about a project's files, served over MCP.

### Added

#### Indexing

- Incremental indexing. An ignore-aware walk hashes files and reparses only what
  changed; deleted files are pruned, and graph resolution re-runs each time.
- The ignore-aware walk honors `.gitignore` negation and directory-only
  patterns: patterns apply in order (a later match wins) so a `!` line
  re-includes a subtree, and a trailing `/` restricts a pattern to directories.
  This is what lets a JS/TS monorepo that ignores a tree but keeps its source
  (`packages/*/*/` then `!packages/*/src/`) be indexed at all -- previously the
  whole `packages/*/src` source tree, and same-depth manifests like
  `packages/<pkg>/package.json`, were skipped (on tRPC: 809 -> 1470 files).
- A binary upgrade forces a full re-parse: the index records the binary's version,
  so extractor and resolver fixes apply on update instead of incremental hashing
  skipping unchanged files and serving stale data. Release builds key this off the
  commit; dev and dirty builds key off the binary's mtime so each local rebuild
  also reparses.
- Tree-sitter extraction for Go, Rust, Java, Ruby, C#, TypeScript/TSX, Lua,
  Python, JavaScript, Bash, Fish, C/C++, QML, CSS, SCSS, Markdown, JSON, YAML, TOML, INI, and Hyprland,
  plus a line-based reader for other config formats (sway/i3, rofi `rasi`,
  polybar, and similar). Markdown headings and JavaScript declarations become
  symbols, so docs and dashboard scripts are searchable by name as well as by
  content. Go, Rust, and TypeScript are indexed at the symbol level (functions,
  methods, types, structs/enums/traits/interfaces, and import edges), so `find`
  and `search` cover those projects; prowl can now index its own Go source.
- Go package graph: an in-module import resolves to every file of the imported
  package (synthetic `pkg` edges, rebuilt each resolve so they never accumulate),
  so `callers`, `callees`, `impact`, `changed`, `clusters`, and `entrypoints`
  work across a Go module. The module path comes from `go.mod`; standard-library
  and external imports stay informational and are not flagged. The fan-in/out
  health check excludes `pkg` so a widely-imported core package is not mistaken
  for a risk.
- TypeScript and JavaScript relative imports resolve to files: `./x` and `../x`
  try `.ts/.tsx/.js/.jsx/.mjs/.cjs` and an `index` file in a directory. A modern
  ESM/NodeNext import that cites a `.js` extension (`./foo.js`) resolves to its
  `.ts`/`.tsx` source, and non-source assets (`./styles.css`) match as-is, so
  `callers`, `impact`, `changed`, and `clusters` work across a TS/React app.
- TypeScript/JavaScript workspace packages: a bare import of a first-party
  monorepo package (`@scope/pkg`, `pkg/subpath`) resolves to that package's
  source by the `src/` convention (`src/index`, `src/<subpath>`, or its directory
  index), reading each `package.json` `name` to map the package to its directory.
  It deliberately ignores `package.json` `exports`/`main`, which point at built
  `dist/` output that is not indexed, so edges land on real source. ESM `.js`
  subpaths resolve to `.ts` the same way relative imports do. External (non-
  workspace) packages stay informational. On the tRPC monorepo this resolved 878
  previously-dangling `@trpc/*` imports and gave the core `@trpc/server` entry a
  real cross-package blast radius (impact `0` -> `345`); on zod it resolves the
  subpath imports (`zod/v4/core` -> `packages/zod/src/v4/core/index.ts`).
- Rust module graph: `mod foo;` declarations resolve to the included file
  (`foo.rs` / `foo/mod.rs`, handling both `mod.rs` and `foo.rs` parent layouts),
  `crate::` imports resolve to the module file under the importing file's crate
  root, and cross-crate `use other_crate::...` resolves to that workspace member
  (its crate name read from each `Cargo.toml`), falling back to the crate's
  `lib.rs` for re-exported items, so the graph works across a single crate and a
  Cargo workspace. `super::`/`self::` and external crates stay informational.
- Python imports resolve to files: absolute (`import a.b`, `from a.b import c`)
  to `a/b.py` or `a/b/__init__.py` (also under `src/`), and relative
  (`from .x import y`, `from ..pkg.mod import z`) against the importing file's
  package, so the graph works across a Python package. A bare `from . import x`
  and third-party imports stay informational.
- C# namespace graph: a `using Foo.Bar;` resolves to every file declaring
  `namespace Foo.Bar` (synthetic `pkg` edges, like Go), so `callers`, `impact`,
  `changed`, and `clusters` work across a C# solution. Framework and third-party
  usings match no declared namespace and stay informational.
- Java import graph: `import com.foo.Bar` resolves to that class's file under any
  module's source root (the part after `src/main/java`, `src/test/java`, or
  `src/`), so a multi-module Gradle/Maven project links across modules; wildcard
  `import com.foo.*` fans out to every file in the package. JDK and third-party
  imports stay informational. On the retrofit repo this took resolved edges from
  105 to 704 and produced real per-module clusters and cross-module blast radius.
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
- `clusters`/`overview` stay a useful subsystem map on cohesive codebases: when
  connected components collapse nearly everything into one giant blob (common for
  a well-linked Go/TS project), the blob is subdivided by directory subsystem
  (e.g. `pkg/gui`, `pkg/commands`) instead of shown as one opaque cluster. Config
  repos with several balanced include-tree components are left as-is. Clusters are
  labeled by directory subsystem (up to two segments), so a monorepo's
  `packages/foo` and `packages/bar` read distinctly instead of both as `packages`.
- SQLite store with FTS5 full-text search and an in-memory BFS blast-radius
  traversal (loads the resolved edge set once and visits each file once), in WAL
  mode so the indexer can write while the server reads. On a 2023-file Go repo a
  blast-radius query runs in ~80 ms.

#### Interface

- Shell query interface (the recommended, lowest-overhead path): `find`, `search`
  (`--smart`/`--compact`), `overview`, `clusters`, `callers`, `callees`,
  `relations`, `impact`, `entrypoints`, `references`, `hotspots`, `violations`,
  `tests`, `changed`, and `doctor` are first-class subcommands. Any agent that
  can run a command can use the index over a plain shell call: no MCP server, no
  `serve`, and none of the upfront tool-schema tokens MCP injects into every
  request. Each call resolves the workspace by walking up to `.prowl/` and
  freshens the index first, so results are current. A cheap file fingerprint
  (paths plus mtimes, no content reads) lets repeated calls skip the read-and-hash
  re-index when nothing changed, keeping the shell path fast on large repos.
- `find` matches an exact symbol name first, then full-text, then a substring
  fallback, so a camelCase or snake_case fragment (`cloud`) still surfaces the
  symbols that contain it (`updateCloudClient`) instead of returning nothing.
- `references <id>` answers "what uses this symbol": config/resource reference
  edges when present, and for code symbols (which have no language call graph) it
  falls back to full-text call sites of the name, excluding the definition. So
  `find <name>` then `references <id>` returns the call sites instead of nothing.
- `callees` lists what a file directly imports/execs/binds, not the package
  fan-out: a Go file importing N packages shows N imports, not one row per file
  in each imported package. On a 47-import file this is ~50 rows, not ~400.
  `callers` keeps the fan-out (so cross-package importers still show up). Both,
  plus `relations` and `references` edges, emit a uniform `{file, kind, line,
  raw, resolved}` row and drop internal node ids/types, so the result stays a
  TOON table even when a file mixes resolved and external edges (instead of
  degrading to the verbose per-item form).
- `changed` maps your git changes (working tree vs `HEAD`, or `--base <ref>`) to
  the files each one could affect, summarized per file (dependent count, subsystem
  breakdown, direct importers) like `impact`, so an agent can see the impact of an
  edit before committing without a flood of rows. `--all` includes unindexed paths.
- `impact` summarizes the blast radius by default: a total dependent count, a
  breakdown by subsystem (which surfaces the dependency hubs driving the radius),
  and the direct importers, instead of dumping every dependent. On a large Go
  package this is a 12-line summary rather than a 600+ row list. `--all` lists
  every dependent file.
- `overview` is a compact project map for an agent's first call: per-cluster it
  reports the label, language, and file count rather than every file path (the
  full lists stay in `clusters`), and it surfaces the project's architecture docs
  (root README, ARCHITECTURE, CONTRIBUTING, `docs/**` guides) so the agent reads
  the human-written guide before code. On a 2023-file Go repo this is a ~1.3 KB
  map instead of ~49 KB of mostly file paths.
- `clusters` lists subsystem summaries (label, language, file count) by default,
  and `clusters <name>` returns the files of matching subsystems, so pulling one
  subsystem stays cheap instead of dumping every cluster's files. On a 2023-file
  Go repo the summary is ~0.3 KB instead of ~47 KB.
- Savings are tracked on every delivery path: each shell query, like each MCP
  call, records what it returned versus the size of the files it pointed at, so
  `prowl-agent status` keeps counting when agents use the CLI instead of MCP.
- Token-lean output: query results default to TOON (Token-Oriented Object
  Notation), which encodes uniform result arrays as one header plus CSV-style
  rows. Models read it about 40% cheaper than JSON, and slightly more accurately.
  `--json` switches any query command back to JSON, and `--limit N` caps a result
  list so an agent can ask for the top N and pay for fewer tokens. Symbol results
  carry both `line` and `end_line`, so an agent reads just the symbol's range
  instead of the whole file.
- 17 MCP tools: `overview`, `clusters`, `find_symbol`, `find_references`,
  `find_callers`, `find_callees`, `file_relations`, `blast_radius`,
  `entrypoints_for`, `tests_for`, `similar_code`, `smart_search`,
  `architecture_violations`, `repo_hotspots`, `doctor`, `status`, and `reindex`.
- CLI surface: the setup and maintenance commands `init`, `status`, `doctor`,
  `restart`, `update`, and `version`, plus the read-only query commands above.
  The MCP `serve` and editor `lsp` commands stay hidden because agents and
  editors launch them over stdio; you never run them by hand. `init` is the single
  setup command and is idempotent: re-running it (after a reboot, say) keeps your
  settings instead of re-prompting.
- `restart` rebuilds the structural index from scratch and stops running serve/lsp
  processes so the agent or editor relaunches the current binary; the relaunched
  server re-embeds lazily, so an Ollama or model issue cannot block the restart.
- Setup writes MCP configs: the standard `.mcp.json` (most agents), Cursor, VS Code, Oh My
  Pi (`.omp/mcp.json`), Factory droid (`.factory/mcp.json`), and OpenCode
  (`opencode.json`, its own shape), plus an `AGENTS.md` block; state stays in a
  gitignored `.prowl/` folder. Server entries now include `type: "stdio"` (which
  VS Code requires). The README documents the one-command setup for any other agent.
  The `AGENTS.md` block is delimited by `<!-- prowl-agent -->` markers and only
  that span is ever rewritten, so a re-init refreshes prowl's guidance while
  leaving the user's own AGENTS.md untouched; a malformed block missing its
  closing marker replaces only the marker line, never the user text below it.
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
  (downloaded and checksum-verified), then stops running serve/lsp processes so the
  agent or editor relaunches the new build, so it goes live without a manual
  restart. `prowl-agent status` shows update status ("up to date" or "update
  available") by comparing the build's commit against the latest commit on main
  (cached briefly, recomputed against the running build so it is correct right
  after an update); it works for source builds too via the embedded VCS revision.
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
  git-churn hotspots, with a 0-100 score. The command renders a colored card in a
  terminal (score bar, severity breakdown, per-check counts, grouped findings) and
  plain text when piped; `--json` carries the report.
- Tuned to keep false positives low: references are emitted only for path-shaped
  targets, commands resolve against the project, checks skip lifecycle directories
  (migrations, installers, CI, vendor), and only project-relative broken includes
  are flagged. On one 2172-file project this brought the report from 2052 findings
  down to about 90, most of them real.
- `hotspots` (command and the `repo_hotspots` tool) ranks the largest functions
  by line span and the most complex functions by cyclomatic complexity, alongside
  central and large files: triage for where bugs hide. Rankings cover the
  project's own code; vendored and generated files (`vendor/`, `third_party/`,
  `node_modules/`, `*.pb.go`, ...) stay indexed and queryable but are excluded
  from hotspots as noise. Complexity counts decision points
  (if/for/while/switch-cases/catch/match-arms) in a function body for Go,
  Rust, C++, Java, Ruby, C#, TypeScript/TSX, JavaScript, and Python; other languages report size only.

#### Semantic search (optional)

- A local Ollama layer stores chunk embeddings in sqlite-vec. `similar_code` fuses
  vector and full-text results, and `smart_search` adds a query rewrite and a
  rerank. Both fall back to full-text when the layer is off. The model only
  reorders and rewrites; it never makes decisions and is never its own tool.
- Resilience: if the configured embed model is missing or Ollama is unreachable,
  the server logs a notice and runs structural-only instead of failing to start,
  and an embedding error during a refresh never fails the index.
- `init` sets up the semantic layer end to end: it offers model tiers (`fast` /
  `smart` / `max`, or `--tier`), prefers models already installed on Ollama over
  the tier preset (so it never points the config at an absent model or asks for a
  redundant pull), offers the official Ollama installer, brings the daemon up
  (reusing a service, installing a user service that survives reboot, or spawning
  it), pulls only missing models, and warms the embed model. AI on/off and tier
  persist in a global config so a fresh project inherits your last choice and a
  re-init never silently disables AI; `--reconfigure` re-opens the prompts. Tier
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
