---
description: Answer a repository question from Prowl's cited index instead of grepping and reading many files.
argument-hint: [what you are looking for]
allowed-tools: Bash(prowl-agent:*)
---

Answer this repository question by querying Prowl's index with the read-only
`prowl-agent` CLI, not by grepping or reading whole files:

$ARGUMENTS

Route the request through the canonical `code-search` skill's routing table --
do not restate a separate one. That decision table is the single source of
truth for which command answers which question:

- semantic "where is / how does this work" -> `prowl-agent search "<question>"`
- a named symbol, component, or setting -> `prowl-agent find <name>`
- read one definition -> `prowl-agent def <name-or-id>`
- a file's shape -> `prowl-agent outline <path>`
- callers or references -> `prowl-agent references <name-or-id>`
- change blast radius -> `prowl-agent impact <path>`

Reserve native grep for an exact literal or regex match and native glob for
filename patterns; every semantic or structural question starts with a
`prowl-agent` command. Report each answer with its file:line citation.
