---
name: prowl-repo-exploration
description: Use when reviewing or understanding a code repository, exploring an unfamiliar codebase, finding where a function, symbol, component, setting, or keybind is defined, tracing who calls or imports a file, or extracting how a specific feature works. Reach for prowl-agent before grepping and reading many files to answer these structural questions; keep grep and glob for literal text and filename scans.
---

# Prowl repo exploration

prowl-agent keeps a local index of the repo (files, symbols, and how they
connect) and answers from it in one command, cited to file and line. It replaces
the grep-then-open-many-files loop for structural questions. It does not replace
grep or glob for literal string or filename scans.

## When prowl, when grep

- Use prowl for: "where is X defined", "who calls or imports this", "what breaks
  if I touch this", "what are the subsystems", "how does feature Y work". These
  are questions about structure and relationships.
- Use grep or glob for: a literal string, a regex, a filename pattern, or reading
  one file you have already located.

## Workflow

1. Orient: `prowl-agent overview` (project map, entrypoints, clusters, docs to read).
2. Locate: `prowl-agent find <name>` for a symbol; `prowl-agent search <text>` for content.
3. Understand a slice: `prowl-agent context search "<question>" --budget-tokens 2000`
   returns a bounded, cited packet. Add `--json` to parse it.
4. Trace: `prowl-agent callers <path>`, `prowl-agent callees <path>`,
   `prowl-agent references <id>` (id from find), `prowl-agent impact <path>`.

Output is compact TOON by default; add `--format json` when you need to parse it.
Every query re-indexes what changed first, so answers are current.

## A repo you do not own

To review or extract from a repo without writing anything into it, use
`prowl-agent explore <path> --question "<what you need>"`. It indexes to a scratch
location, answers, and leaves the target tree untouched (no `.prowl/`, no
`.gitignore` or `AGENTS.md` edits).

## If ranking looks noisy

Without a local model, `search`/`context` rank by full text, so a keyword-dense
file (a translation table, a changelog) can outrank the real code. Two fixes,
neither needs the user to install anything:

- Over MCP: call `search_context` with `rerank: true`. prowl asks your own model
  to reorder the candidates. It needs no local model and is ignored when your
  client does not support sampling.
- Otherwise: rerank the returned candidates yourself (a cheap model is enough).
  Drop docs, locale, and generated files; keep implementation.

Either way, do not fall back to grepping the whole tree.
