# Prowl Agent

**One local index that answers your AI agent's questions about a codebase in a single command, cited to `file:line`, for a fraction of the tokens that grep-and-read burns.**

[![build](https://github.com/neur0map/prowl-agent/actions/workflows/release.yml/badge.svg)](https://github.com/neur0map/prowl-agent/actions/workflows/release.yml)
[![version](https://img.shields.io/github/v/release/neur0map/prowl-agent?label=version&color=89b4fa)](https://github.com/neur0map/prowl-agent/releases/latest)
[![platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-555)](#install)

Every time you ask a coding agent to change something, it greps the repo and
re-reads the same files to rebuild context it already lost. You pay for those
tokens on every turn, the answer arrives slower, and the agent still has to guess
how the files connect.

Prowl Agent builds a small SQLite index of your project (the files, the symbols
in them, and how they wire together) and answers from it in one shell command.
The agent runs `prowl-agent find`, `overview`, or `impact` and gets a short,
exact, cited answer instead of a wall of grep hits. Answers come back in
[TOON](https://toonformat.dev), a format models read about 40% cheaper than JSON.

```console
$ prowl-agent find NewGui            # exact hits with signatures, all cited
[4]{end_line,file,id,kind,line,name,signature}:
  266,pkg/gocui/gui.go,2709,function,212,NewGui,"func NewGui(opts NewGuiOpts) (*Gui, error)"
  799,pkg/gui/gui.go,4977,function,723,NewGui,"func NewGui( cmn *common.Common, configurer config.AppConfigurer, ... )"
  44,pkg/commands/oscommands/gui_io.go,2314,function,32,NewGuiIO,"func NewGuiIO( log *logrus.Entry, ... ) *guiIO"
  209,pkg/gocui/gui.go,2708,type,198,NewGuiOpts,"NewGuiOpts struct { OutputMode OutputMode ... }"

$ prowl-agent impact pkg/gui/gui.go  # who breaks if I touch this file
total: 7
direct: 2
by_subsystem[5]{count,subsystem}:
  2,pkg/cheatsheet
  2,pkg/integration
  ...
```

The grep version of the first question dumps a hit list, then the agent opens
each file to find the right `NewGui` and read its signature: kilobytes to tens of
kilobytes just to locate one symbol. Prowl answers in under a kilobyte and hands
the agent each symbol's signature inline, so it knows which one to call without
opening a file.

## Install

Linux and macOS (amd64 or arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/neur0map/prowl-agent/main/install.sh | sh
```

Windows amd64 from PowerShell:

```powershell
irm https://raw.githubusercontent.com/neur0map/prowl-agent/main/install.ps1 | iex
```

Both installers select the native artifact and verify its SHA-256 checksum.
Linux builds need a recent glibc. To build from source, install Go 1.26+, a C
compiler, and SQLite development headers (`libsqlite3-dev` on Debian/Ubuntu):

```sh
CGO_ENABLED=1 go build -tags sqlite_fts5 -o prowl-agent ./cmd/prowl-agent
```

Update in place anytime with `prowl-agent update`. `prowl-agent status` also
tells you when a new build is out, via a quick anonymous checksum check cached
for a day.

## Set up in one command

Run this once inside any project (a code repo, a dotfiles folder, `~/.config`):

```sh
prowl-agent init                                      # interactive client selection
prowl-agent init --dry-run --integrations auto        # exact preview, no writes
prowl-agent init --no-ai --no-input --integrations cursor,vscode
```

`init` builds the index, previews the selected integrations, and writes only the
clients you choose. `--integrations auto` selects clients already present in the
project; use `none`, `all`, or a comma-separated list. `--remove-integrations`
removes only Prowl-owned entries and preserves neighboring user configuration.
Core state lives in `.prowl/`, which is added to `.gitignore`. Nothing leaves
your machine.

Setup is transactional: malformed client configuration aborts the operation and
restores files already touched. The optional `AGENTS.md` guidance is delimited by
markers, so re-running setup refreshes only Prowl's block.

There is no server to keep running. Each query re-indexes incrementally first
(only what changed, in tens of milliseconds), so the agent never reads stale data
and you never run a watcher by hand. The optional human workbench runs only for
the current terminal session:

```sh
prowl-agent open                 # choose a free loopback port and open the browser
prowl-agent open --no-browser    # print the local URL without launching a browser
prowl-agent open --port 43117    # request a specific loopback port
```

`open` binds only to `127.0.0.1`. It generates a 256-bit in-memory bearer token,
passes it in the initial URL fragment, and the frontend immediately removes that
fragment from browser history. Project APIs require the token and exact loopback
host, and reject any non-exact browser `Origin`; the token is never stored in
cookies or local storage. Stop the session with Ctrl-C.

## What your agent can ask

After `init`, the agent (or you) queries the index by running a command. This is
the lowest-overhead path: no server, and none of the tool-schema tokens an MCP
server adds to every request.

```sh
prowl-agent overview            # project map: docs to read, roles, entrypoints, clusters (start here)
prowl-agent find <name>         # locate a symbol (function, setting, keybind, component)
prowl-agent search <text>       # search content; --smart reranks, --compact lists files only
prowl-agent callers <path>      # what includes / imports / execs / binds to a file
prowl-agent callees <path>      # what a file includes / imports / execs / binds to
prowl-agent impact <path>       # blast radius: count, subsystems, direct importers (--all = full list)
prowl-agent relations <path>    # a file's symbols and include neighbors
prowl-agent entrypoints <path>  # root files from which this file is reachable
prowl-agent references <id>     # references to a symbol id (the id column from find)
prowl-agent clusters [name]     # subsystems (summaries); with a name, that subsystem's files
prowl-agent hotspots            # structurally central / large / complex files
prowl-agent violations          # dangling refs, orphan scripts, hardcoded colors
prowl-agent doctor              # general health: cycles, fan risk, dangling refs, score
prowl-agent doctor --profile rice # add keybind, desktop command, color, orphan checks
prowl-agent tests <path>        # configs/keybinds that launch or reload a file
prowl-agent changed             # your git changes mapped to the files they could affect
prowl-agent context search <question> --mode compact --budget-tokens 1800 --json
prowl-agent capabilities search <query> # discover workflows without loading every schema
```

TTY output defaults to a human view; pipes and agent calls default to compact
TOON. Choose `--format human|toon|json|markdown` explicitly (`--json` remains a
compatibility alias), or `--limit N` to cap results. Run from anywhere inside
the project; Prowl finds the index by walking up to `.prowl/`. Each answer is built
to stay small: on a 2023-file Go repo, `overview` is about 1 KB and a typical
`impact` answer is a dozen lines, not the few hundred dependent rows the raw
graph would print.

## Keep durable project knowledge

Accepted concepts, decisions, claims, and playbooks live as portable OKF Markdown—not inside the disposable SQLite index:

```bash
prowl-agent knowledge init
prowl-agent knowledge propose --file candidate.md --target decisions/storage.md
prowl-agent knowledge accept <proposal-id>
prowl-agent knowledge list
prowl-agent knowledge lint
prowl-agent knowledge export ./knowledge-export
```

Every proposal exposes a deterministic diff before acceptance. Source anchors report stale evidence when the selected code region changes. Unknown OKF v0.1 fields and future concept types round-trip without loss.

See [Durable knowledge and OKF](docs/KNOWLEDGE.md) for the storage contract, review lifecycle, lint codes, and migration safeguards.

## One index, three ways to use it

The same `.prowl/index.db` is served three ways, so you pick the integration that
fits and the answers stay identical and cited:

- **Shell commands (recommended).** Any agent that can run a command can use
  prowl. Nothing to start, and none of MCP's upfront per-call tool-schema cost.
- **MCP server.** For agents that prefer typed tools, select the standard
  `.mcp.json`, Cursor, VS Code, Oh My Pi, Factory droid, or OpenCode integration
  during setup. Compatibility mode exposes the historical 17 tools; MCP Resources
  and Prompts are additive on every surface. Use `prowl-agent serve --mcp-surface
  core` for six intent-oriented tools, or `--mcp-surface all` during migration.
  Point any other agent at one command, `prowl-agent serve`.
- **Editor language server.** `prowl-agent lsp` gives a human go-to-definition,
  find-references, hover (with use counts), document and workspace symbols, code
  lens, completion, and inline `doctor` diagnostics. Neovim attaches it
  automatically; Helix and VS Code setup notes are in `.prowl/editor/SETUP.md`.

```json
{
  "mcpServers": {
    "prowl-agent": { "type": "stdio", "command": "prowl-agent", "args": ["serve"] }
  }
}
```

See [Context packets and MCP v2](docs/CONTEXT.md) for packet fields, resource
URIs, prompts, compatibility modes, privacy-safe traces, capability discovery,
and the deterministic retrieval evaluation.

## It understands how code connects

Locating a symbol is the easy half. The harder, more valuable half is the graph:
what imports what, what a change ripples into, which files form a subsystem.
Prowl resolves real edges, not text matches:

- **Code imports.** Go package imports, TypeScript/JavaScript relative imports,
  Rust `mod` and `crate::` imports, Python absolute and relative imports, C/C++
  `#include`, Java and Kotlin `import` class paths (which resolve to each other
  in a mixed JVM project, and fold a member or nested-type import to its
  enclosing class file), Ruby `require_relative`, C# `using` namespaces, PHP
  `use Ns\Class` imports (resolved to the file declaring that class), Dart
  `package:` and relative imports (resolved to a workspace package's `lib/`), and
  Elixir `alias`/`import`/`use` (resolved to the file declaring that module).
- **Monorepos and path aliases.** A bare import of a first-party workspace
  package (`@scope/pkg` or `pkg/subpath`) resolves to that package's source, and
  a tsconfig path alias (`@/components/Button` with `"paths": {"@/*": ["src/*"]}`)
  resolves to the real file, scoped to the nearest `tsconfig.json` so a monorepo's
  per-package aliases stay correct. So `callers`, `impact`, and `clusters` work
  across a pnpm/turbo/Next.js project, not just within one package. The walk
  honors `.gitignore` negation, so a repo that ignores a tree but keeps its
  source (`packages/*/*/` then `!packages/*/src/`) is still indexed.
- **Configs.** Include trees (`source=`, `@import`, `require()`), exec and keybind
  chains (`exec-once`, `bind = ... exec script`), and shared colors, fonts, paths,
  and variables across files.

This is tested against real, popular repositories. A few results:

| repo | language | what the graph now sees |
|---|---|---|
| [lazygit](https://github.com/jesseduffield/lazygit) | Go, 2023 files | `impact`, `callers`, `clusters` across every package |
| [tRPC](https://github.com/trpc/trpc) | TS monorepo | 878 cross-package imports resolved; `impact @trpc/server` went 0 to 345 dependents |
| [zod](https://github.com/colinhacks/zod) | TS subpaths | `zod/v4/core` resolves to `packages/zod/src/v4/core/index.ts` |
| [Laravel](https://github.com/laravel/framework) | PHP, 2955 files | 8237 `use` imports resolved across components; `impact Str.php` reaches 1527 dependents |
| [OkHttp](https://github.com/square/okhttp) | Kotlin + Java | 1944 imports resolved across Kotlin-Multiplatform source sets, including cross-language and companion-member imports |
| [LocalSend](https://github.com/localsend/localsend) | Dart/Flutter | 977 `package:` imports resolved across a multi-package workspace; `impact` on a shared DTO reaches 73 files |
| [shadcn/ui](https://github.com/shadcn-ui/ui) | TS, 8869 files | 8382 `@/` tsconfig-alias imports resolved across many per-package configs |

External and standard-library imports stay informational. More languages are on
the way.

## What it costs to run

`prowl-agent status` prints a card with what is indexed and, once your agent has
asked a few questions, how many tokens it saved. The number is grounded per
answer: for each query prowl served, it compares the bytes it returned against
the combined size of the files that answer pointed at (what an agent would
otherwise have read), then keeps about 70% of that as a deliberately
under-counted estimate. It tracks every project you have initialized and shows a
combined total.

Run it in your terminal for the full colored card; pipe it for plain text, or add
`--json` for the raw numbers. Want to check the math on your own repos? See
[Measuring token usage](docs/TOKENS.md).

A rough idea of the gap, from a small test on three real dotfile repos (not a
benchmark suite): indexed and asked the same question, `find_symbol` returned
about 5 KB of cited results in a couple of milliseconds. The plain ripgrep hit
list was around 50 KB, and opening every file it matched ran to a few megabytes.
A few thousand tokens versus tens of thousands, just to locate something, before
the agent reads anything. Your files, your question, and your editor move these
numbers, so measure on your own setup.

## Your code stays on your machine

Prowl indexes only what your project tracks (it honors `.gitignore`) and keeps
its own state in a local `.prowl/` folder, which it adds to `.gitignore`. The
update check is an anonymous read of public commit data and sends nothing about
you. There is no daemon and no network service.

Gitignoring `.prowl/` does not hide your code from the agent: the agent reads your
real files, and `.prowl/` only holds the rebuildable index. Because prowl indexes
the same files git tracks, it never points the agent at a path it was told to
ignore.

## Optional: semantic search

If you turn it on, `init` walks you through a local semantic layer powered by
[Ollama](https://ollama.com), with no cloud and no API keys. You pick a tier;
`init` detects Ollama, starts it, pulls the models, and warms the embed model so
the first query is hot:

| tier | embed | assist | needs |
|---|---|---|---|
| fast | `embeddinggemma` | `gemma3:1b` | runs anywhere, CPU ok |
| smart | `qwen3-embedding:4b` | `gemma4:e2b` | ~10 GB VRAM |
| max | `qwen3-embedding:8b` | `gemma4:e4b` | ~16 GB VRAM |

Choose non-interactively with `--tier fast|smart|max`. The tiers differ mainly in
the embedder, which is where recall comes from: a bigger embedder finds related
code on large repos or vaguely worded questions that a small one misses. The
assist model only rewrites and re-ranks, so it stays small on purpose.
Embeddings live in `sqlite-vec`, so the agent finds files that mean the same
thing even when they share no words (for example, "music spectrum" finds an
`AudioVisualizer`). Structural search works without any of this.

## Supported formats

Go, Rust, Java, Kotlin, Ruby, C#, PHP, Dart, Elixir, TypeScript/TSX, Lua, Python,
JavaScript, Bash, Fish, C/C++, QML, CSS/SCSS, Markdown, TOML, YAML, JSON/JSONC, INI, and Hyprland
(`hyprlang`), plus a line-based reader for everything else (sway/i3, rofi `rasi`,
polybar, kitty, dunst, and similar).

## More

- [Architecture](docs/ARCHITECTURE.md): how indexing, the graph, and the server fit together
- [Measuring token usage](docs/TOKENS.md): how the savings number is computed, and how to check it
- [Changelog](CHANGELOG.md)

Built with Go, Tree-sitter, and SQLite for Linux, macOS, and Windows.
