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

Lua, Python, Bash, Fish; C++; CSS, SCSS; TOML, YAML, JSON/JSONC, INI; QML;
Hyprland (`hyprlang`); plus a line-oriented fallback for bespoke WM configs
(sway/i3, rofi `rasi`, polybar, kitty, dunst, and similar).

## Requirements

Linux, Go 1.26+, and a C toolchain (cgo is required for Tree-sitter and SQLite).

## Install

Download the latest Linux x86_64 binary from the rolling release (rebuilt on every
push to `main`):

```sh
curl -fsSL -o ~/.local/bin/prowl-agent \
  https://github.com/neur0map/prowl-agent/releases/download/nightly/prowl-agent-linux-amd64
chmod +x ~/.local/bin/prowl-agent
prowl-agent --version
```

A `.sha256` is published alongside the binary. The build is cgo-linked and needs a
recent glibc. Or build from source (below).

## Build

```sh
CGO_ENABLED=1 go build -tags sqlite_fts5 -o prowl-agent ./cmd/prowl-agent
```

The `sqlite_fts5` build tag enables SQLite's FTS5 full-text engine.

## Usage

The command surface is intentionally small: `init`, `status`, `doctor`, `help`.

```sh
# In your dotfiles repo (or ~/.config), set everything up:
prowl-agent init                 # interactive setup wizard
prowl-agent init --no-ai --yes   # non-interactive

# Inspect index state for this project, or list all initialized projects:
prowl-agent status
prowl-agent status --json

# Diagnose rice health (cycles, keybind conflicts, dead scripts, broken commands):
prowl-agent doctor
```

`init` creates a per-folder `.prowl/` workspace (`config.toml`, `rules.toml`,
`index.db`), runs the first index, registers the MCP server (`.mcp.json`,
`.cursor/mcp.json`, `.vscode/mcp.json`),
writes agent instructions into `AGENTS.md`, and adds `.prowl/` to `.gitignore`.
The index database and other backend state never leave `.prowl/`.

The MCP server is launched by your coding agent through the generated config
(it runs the hidden `prowl-agent serve` over stdio); you do not run it by hand.
While running, it watches the rice and re-indexes changed files automatically, so
agent context stays fresh.

## MCP tools

`overview`, `clusters`, `find_symbol`, `find_references`, `find_callers`,
`find_callees`, `file_relations`, `blast_radius`, `entrypoints_for`, `tests_for`,
`similar_code`, `smart_search`, `architecture_violations`, `repo_hotspots`,
`doctor`, `status`,
and `reindex`. Structural results are deterministic and carry `file:line`
provenance.

## Benchmarks

Project: [github.com/neur0map/prowl-agent](https://github.com/neur0map/prowl-agent).
Test repo: [neur0map/ryoku-arch](https://github.com/neur0map/ryoku-arch), a real
Arch rice with **2814 tracked files** (2172 indexed, 111,977 symbols). Task: find
the files that make up the plugin system. Tokens are bytes/4 (approx).

| metric | without prowl (`rg "plugin"`) | prowl structural (MCP) | prowl + local AI |
|---|---|---|---|
| Per-query latency | ~1.9 s (rescans) | 1-16 ms | 37 ms `similar_code`, 6.3 s `smart_search` |
| Output to read | 288 KB / ~74k tokens | 0.4-12 KB / ~0.1-3k tokens | ~13 KB / ~3k tokens |
| One-time index build | none | ~14 s | ~14 s + ~121 s embeddings |
| Files returned | 121, unranked (5 homonyms) | typed, ranked subset | semantically ranked |
| Finds C++/QML runtime (`shell/plugin/`) | no | yes (`clusters`) | yes |
| Semantic match (no shared words) | no | no | yes ("music spectrum" finds `AudioVisualizer.qml`) |
| Change impact (`blast_radius`) | no | yes (<1 ms) | yes |

Structural search is ~95% fewer tokens and 100-1000x lower per-query latency than
re-grepping. The local-AI layer adds meaning-based recall (files that share no
keywords) for a one-time embed cost; see Semantic search below for model lifecycle.

### Doctor accuracy

`doctor` is tuned to keep false positives near zero. On ryoku-arch its report
dropped from **2052 raw findings to 90** after: resolving bare commands against
the repo's `bin/`, scoping checks to rice files (skipping migrations / installers
/ CI / vendor), treating `~` / system / URL targets as runtime (not broken), only
flagging repo-relative broken includes, and excluding data files from the size
check. The remaining findings are real (broken theme imports, a monolithic
`config.cpp` referencing 21 files, churn hotspots, hardcoded colors).

## Semantic search (opt-in)

The setup wizard can enable a local semantic layer (via Ollama). When enabled,
chunk embeddings (`qwen3-embedding:0.6b` by default) are stored in `sqlite-vec`,
and `similar_code` fuses vector nearest-neighbor search with full-text search
(reciprocal rank fusion). A small assist model (`gemma3:4b` by default) stays a
retrieval helper only: it never makes decisions and is never exposed as its own
tool. Structural search works fully without any of this.

**Model lifecycle:** the model is not cold-started per query. `serve` embeds the
index once at startup (warming the embed model), and Ollama keeps a model
resident for a keep-alive window (default ~5 min) after each use. Measured here:
first embed after idle ~2.4 s (model load), then warm embeds ~20 ms; the assist
model (used only by `smart_search`) warms the same way. So a model cold-starts
once per idle period, then stays hot during active MCP use. Set
`OLLAMA_KEEP_ALIVE=-1` to keep it resident permanently.

## Architecture

```
cmd/prowl-agent      entry point (cobra)
internal/parse       Tree-sitter grammar loading and per-language extractors
internal/graph       include / exec / resource resolution and role inference
internal/index       ignore-aware walk and hash-based incremental indexing
internal/store       SQLite schema, FTS5, sqlite-vec, graph reads (blast-radius CTE)
internal/query       structural query ops + hybrid/semantic search
internal/doctor      health diagnostics (cycles, conflicts, hotspots)
internal/mcp         MCP stdio server
internal/cli         init wizard, status, doctor, hidden serve + file-watcher, injection
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
