# Prowl Agent

A local index that lets AI coding agents understand your Linux rice without re-reading the whole thing.

[![build](https://github.com/neur0map/prowl-agent/actions/workflows/release.yml/badge.svg)](https://github.com/neur0map/prowl-agent/actions/workflows/release.yml)
[![download](https://img.shields.io/github/v/release/neur0map/prowl-agent?include_prereleases&label=download)](https://github.com/neur0map/prowl-agent/releases/latest)
![platform](https://img.shields.io/badge/platform-Linux-555)

Coding agents burn a lot of tokens grepping and re-reading config files every time
you ask them to tweak your bar, your keybinds, or a theme. Prowl Agent builds a
small SQLite index of your dotfiles (configs, widgets, scripts, themes, colors) and
serves it to the agent over the [Model Context Protocol](https://modelcontextprotocol.io).
The agent asks a question and gets a short, exact answer with `file:line` links
instead of a pile of grep hits.

It knows how a rice is wired together:

- include trees (`source=`, `include`, `@import`, `require()`)
- exec and keybind chains (`exec-once`, `bind = ... exec script`)
- shared colors, fonts, paths, and theme variables across files

## Install

Grab the latest Linux x86_64 build (rebuilt on every push to `main`):

```sh
curl -fsSL -o ~/.local/bin/prowl-agent \
  https://github.com/neur0map/prowl-agent/releases/download/nightly/prowl-agent-linux-amd64
chmod +x ~/.local/bin/prowl-agent
prowl-agent --version
```

A `.sha256` sits next to the binary. It is cgo-linked, so it needs a recent glibc.
To build from source instead, you need Go 1.26+ and a C toolchain:

```sh
CGO_ENABLED=1 go build -tags sqlite_fts5 -o prowl-agent ./cmd/prowl-agent
```

## Quick start

Run this once inside your dotfiles repo (or `~/.config`):

```sh
prowl-agent init                 # interactive setup
prowl-agent init --no-ai --yes   # or non-interactive
```

That builds the index, registers the MCP server for Cursor, VS Code, and any
MCP-compatible agent, and writes a short `AGENTS.md`. Everything lives in a local
`.prowl/` folder that gets added to `.gitignore`. Nothing leaves your machine.

Two commands are handy day to day:

```sh
prowl-agent status   # what is indexed
prowl-agent doctor   # broken includes, dead scripts, keybind clashes
```

The agent launches the server itself through the generated config, watches your
files, and re-indexes on save, so its answers stay current.

## What the agent can ask

Once it is running, the agent has tools to:

- **find things** by name or meaning (`find_symbol`, `similar_code`, `smart_search`)
- **follow connections** (`find_callers`, `find_callees`, `file_relations`, `entrypoints_for`)
- **check impact** before a change (`blast_radius`, `repo_hotspots`)
- **get the lay of the land** (`overview`, `clusters`)
- **spot problems** (`doctor`, `architecture_violations`)

Every answer is deterministic and comes with `file:line`, so the agent (and you)
can verify it.

## Benchmarks

We indexed three real, sizable rices and asked the same question on each: find the
battery widget and the files around it.

- [ryoku-arch](https://github.com/neur0map/ryoku-arch) (2172 files indexed)
- [end-4/dots-hyprland](https://github.com/end-4/dots-hyprland) (732)
- [noctalia-dev/noctalia-shell](https://github.com/noctalia-dev/noctalia-shell) (578)

Averaged across the three:

| | ripgrep | Prowl Agent |
|---|---|---|
| to locate the feature | ~52 KB of raw matches (~13k tokens) | ~5.5 KB ranked answer (~1.4k tokens) |
| per query | re-scans the repo (~60 ms here) | ~1 ms, already indexed |
| what you get | every match, unranked, all meanings mixed in | typed and ranked, with `file:line` |

That is about **10x less to read** than the grep hit list. And because the answer
is ranked with `file:line`, the agent opens the two or three files that matter
instead of the dozens that merely mention the word. Opening everything grep found
would have meant ~2.7 MB on average (~710k tokens).

<details>
<summary>How we counted tokens</summary>

Tokens are estimated as characters / 4, the usual rough rule. The ripgrep figure
is the size of the hits it prints (one `path:line` per match). The "open
everything" figure is the combined size of every file that contained the word,
which is what an agent ends up reading to understand them. The Prowl figure is the
size of the JSON its `find_symbol` tool returns. Same word, same machine, averaged
over the three repos above. A broader semantic search (`similar_code`) returns
about 12 KB (~3k tokens) on the same query.

</details>

## Semantic search (optional)

If you turn it on, `init` sets up a local semantic layer through
[Ollama](https://ollama.com), with no cloud and no API keys. It stores embeddings
in `sqlite-vec` so the agent can find files that mean the same thing even when they
share no words (for example, "music spectrum" finds an `AudioVisualizer`). A small
helper model can rewrite and re-rank queries, but it never makes decisions on its
own. Structural search works fully without any of this.

The model warms up once when the server starts and stays loaded for a few minutes
between queries, so it is not paying a cold start every time (about 2.4 s on the
first query after idle, then around 20 ms).

## Supported formats

Lua, Python, Bash, Fish, C++, QML, CSS/SCSS, TOML, YAML, JSON/JSONC, INI, and
Hyprland (`hyprlang`), plus a line-based reader for everything else (sway/i3, rofi
`rasi`, polybar, kitty, dunst, and similar).

## More

- [Architecture](docs/ARCHITECTURE.md): how indexing, the graph, and the server fit together
- [Changelog](CHANGELOG.md)

Linux only. Built with Go, Tree-sitter, and SQLite.
