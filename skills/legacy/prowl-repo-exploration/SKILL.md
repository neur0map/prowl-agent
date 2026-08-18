---
name: prowl-repo-exploration
description: Use when reviewing or understanding a code repository, exploring an unfamiliar codebase, finding where a function, symbol, component, setting, or keybind is defined, tracing who calls or imports a file, or extracting how a specific feature works. Reach for prowl-agent before grepping and reading many files to answer these structural questions; keep grep and glob for literal text and filename scans.
---

# Prowl repo exploration

prowl-agent keeps a local index of the repo (files, symbols, and how they
connect) and answers from it in one command, cited to file and line. It replaces
the grep-then-open-many-files loop for structural questions. It does not replace
grep or glob for literal string or filename scans.

**Two interfaces, one index.** When your harness exposes Prowl as MCP tools
(search_context, read_symbol, outline, find_references, analyze_change,
sketch_ui), prefer those -- they are already in your tool list. The prowl-agent
CLI below is the opt-in equivalent for when MCP is not wired. Either way, reach
for Prowl before grepping and reading many files.

## When prowl, when grep

- Use prowl for: "where is X defined", "who calls or imports this", "what breaks
  if I touch this", "what are the subsystems", "how does feature Y work". These
  are questions about structure and relationships.
- Use grep or glob for: a literal string, a regex, a filename pattern, or reading
  one file you have already located.

## Workflow

1. Orient: `prowl-agent overview` (whole-project map: entrypoints, clusters, hotspots, docs to read), or `prowl-agent brief <path>` for one subsystem (its size, languages, guides, and the key files to read first, ranked by dependency centrality).
2. Locate & read cheaply: `prowl-agent find <name>` for a symbol; `prowl-agent outline <path>` for a file's structure (symbols and signatures, no bodies); `prowl-agent def <name>` to read only one symbol's source instead of the whole file; `prowl-agent search <text>` for content.
3. Understand a slice: `prowl-agent context search "<question>" --budget-tokens 2000`
   returns a bounded, cited packet. Add `--json` to parse it.
4. Trace: `prowl-agent callers <path>`, `prowl-agent callees <path>`,
   `prowl-agent references <name>` (a symbol's call sites), `prowl-agent impact <path>`.
5. See a UI: `prowl-agent sketch <component-or-path>` renders how a UI looks and behaves without a screenshot or running it -- QML and React (jsx/tsx) as an element tree with visual properties, handlers, and animations; a Go/lipgloss TUI or CSS as its palette, styles, and design tokens -- so you understand or replicate a screen without reading the whole file.

Output is compact TOON by default; add `--format json` when you need to parse it.
Every query re-indexes what changed first, so answers are current. `overview`
also refreshes the always-on project map in `AGENTS.md`, so that passive context
stays current for the next turn.

## A repo you do not own

To review or extract from a repo without writing anything into it, use
`prowl-agent explore <path> --question "<what you need>"`. It indexes to a scratch
location, answers, and leaves the target tree untouched (no `.prowl/`, no
`.gitignore` or `AGENTS.md` edits).

## If ranking looks noisy

prowl already down-weights low-signal files (locale and i18n tables, generated
code, lockfiles, minified bundles, and prose docs) so they rank below the real
code for a code question, unless your query names that class. `hotspots` and the
`overview` rank by dependency centrality, not raw frequency. If a
relevance-sensitive question still ranks poorly:

- Over MCP: call `search_context` with `rerank: true`. prowl asks your own model
  to reorder the candidates. It needs no local model and is ignored when your
  client does not support sampling.
- Otherwise: rerank the returned candidates yourself (a cheap model is enough).

Either way, do not fall back to grepping the whole tree.
