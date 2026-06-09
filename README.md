# Prowl Agent

A local index that helps AI coding agents understand a project without re-reading the whole thing.

[![build](https://github.com/neur0map/prowl-agent/actions/workflows/release.yml/badge.svg)](https://github.com/neur0map/prowl-agent/actions/workflows/release.yml)
[![download](https://img.shields.io/github/v/release/neur0map/prowl-agent?include_prereleases&label=download)](https://github.com/neur0map/prowl-agent/releases/latest)
![platform](https://img.shields.io/badge/platform-Linux-555)

Coding agents spend a lot of tokens grepping and re-reading files every time you
ask them to change something. Prowl Agent builds a small SQLite index of your
project (files, the symbols in them, and the values they share) and serves it to
the agent over the [Model Context Protocol](https://modelcontextprotocol.io). The
agent asks a question and gets a short, exact answer with `file:line` links instead
of a wall of grep hits.

It maps how files are wired together:

- include trees (`source=`, `include`, `@import`, `require()`)
- exec and keybind chains (`exec-once`, `bind = ... exec script`)
- shared colors, fonts, paths, and variables across files

It serves two front ends from one index: the MCP server for your coding agent,
and a language server (`prowl-agent lsp`) for your editor, so a human gets the
same go-to-definition, references, and hover.

Today it is tuned for Linux dotfiles and configs (window managers, bars, widgets,
themes, scripts). Broader language support, including web and more scripting
languages, is in progress.

## Install

Install with one line. It downloads the binary, verifies its checksum, and drops
it in `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/neur0map/prowl-agent/main/install.sh | sh
```

It is a Linux x86_64, cgo-linked binary, so it needs a recent glibc. Prefer to do
it by hand, or build from source? Both work:

```sh
# manual download + checksum verify
curl -fsSL -O https://github.com/neur0map/prowl-agent/releases/download/nightly/prowl-agent-linux-amd64
curl -fsSL -O https://github.com/neur0map/prowl-agent/releases/download/nightly/prowl-agent-linux-amd64.sha256
sha256sum -c prowl-agent-linux-amd64.sha256 && install -m755 prowl-agent-linux-amd64 ~/.local/bin/prowl-agent

# from source (Go 1.26+, C toolchain)
CGO_ENABLED=1 go build -tags sqlite_fts5 -o prowl-agent ./cmd/prowl-agent
```

Update in place anytime with `prowl-agent update`. `prowl-agent status` also tells
you when a new build is out, via a quick anonymous checksum check cached for a day.

## Quick start

Run this once inside your project (a dotfiles repo, `~/.config`, or any folder):

```sh
prowl-agent init                 # interactive setup
prowl-agent init --no-ai --yes   # or non-interactive
```

That builds the index, registers the MCP server for Cursor, VS Code, and any
MCP-compatible agent, wires your editor's language server, and writes a short
`AGENTS.md`. Everything lives in a local `.prowl/` folder that gets added to
`.gitignore`. Nothing leaves your machine.

Two commands are handy day to day:

```sh
prowl-agent status   # index, token savings, and update notice
prowl-agent doctor   # broken includes, dead scripts, keybind clashes
prowl-agent update   # upgrade to the latest build
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

## Use it in your editor

The same index drives a language server, so a human gets the navigation the agent
has. `init` sets it up; the server runs as `prowl-agent lsp`.

- **go to definition**: a keybind to the script it runs, an `@import` or `source=`
  to the file, a `$variable` to where it is declared
- **find references**: every place a color, font, variable, or script is used
- **hover**: a value and how many times it is used
- **document and workspace symbols**, **code lens** (use counts), **completion** of
  known variables and colors, and **inline diagnostics** from `doctor`

Neovim attaches it automatically (see `.prowl/editor/nvim.lua`); Helix gets a
project-local `.helix/languages.toml` when there is none to overwrite. Setup notes,
including VS Code, are in `.prowl/editor/SETUP.md`.

## Does it work in any repo?

Yes. Point it at a dotfiles repo, `~/.config`, or any project folder. It indexes
what the project tracks (it honors `.gitignore`) and keeps its own state in a local
`.prowl/` folder, which it adds to `.gitignore`.

Gitignoring `.prowl/` does not hide your code from the agent. The agent reads your
real files; `.prowl/` only holds the rebuildable index, which the agent never opens
directly (it asks prowl over MCP, and your editor asks over LSP). Because prowl
indexes the same files git tracks, it never points the agent at a path it was told
to ignore.

## See your savings

`prowl-agent status` prints a card with what is indexed and, once your agent has
asked a few questions, how many tokens it saved. The number is grounded per
answer: for each query prowl served, it compares the bytes it returned against the
combined size of the files that answer pointed at (what an agent would otherwise
have read). It is labeled an estimate, because it is one, and it grows as you use
the tool. Run it in your terminal for the full colored card; pipe it for plain
text, or add `--json` for the raw numbers.

## A quick measurement

This is a small test on three real dotfile repos, not a benchmark suite, so take
it as a rough idea rather than a promise. We indexed each and asked the same
question: find the battery widget and the files near it.

| repo | files indexed |
|---|---|
| [ryoku-arch](https://github.com/neur0map/ryoku-arch) | 2172 |
| [end-4/dots-hyprland](https://github.com/end-4/dots-hyprland) | 732 |
| [noctalia-dev/noctalia-shell](https://github.com/noctalia-dev/noctalia-shell) | 578 |

On average, `find_symbol` returned about 5 KB of JSON with `file:line` links, in a
couple of milliseconds against the prebuilt index. For the same word, a plain
ripgrep hit list was around 50 KB, and opening every file it matched added up to a
few megabytes. As a rough idea, that is a few thousand tokens versus tens of
thousands just to locate something, before the agent reads anything.

The point is not a headline number. It is that the agent reads a small, ranked
answer instead of scanning the repo and paging through everything that mentions a
word. Your files, your question, and your editor will all move these numbers, so
measure on your own setup.

<details>
<summary>How we counted tokens</summary>

Tokens are estimated as characters / 4, the common rough rule, so they are
approximate. The ripgrep figure is the size of the hits it prints (one `path:line`
per match). The "open everything" figure is the combined size of every file that
contained the word. The Prowl figure is the size of the JSON `find_symbol`
returns. Same word, same machine, averaged over the three repos. Indexing them
took a few seconds to a few tens of seconds depending on size.

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
first query after idle, then around 20 ms on the repo we tried).

## Supported formats

Lua, Python, Bash, Fish, C++, QML, CSS/SCSS, TOML, YAML, JSON/JSONC, INI, and
Hyprland (`hyprlang`), plus a line-based reader for everything else (sway/i3, rofi
`rasi`, polybar, kitty, dunst, and similar). More languages are on the way.

## More

- [Architecture](docs/ARCHITECTURE.md): how indexing, the graph, and the server fit together
- [Changelog](CHANGELOG.md)

Linux only for now. Built with Go, Tree-sitter, and SQLite.
