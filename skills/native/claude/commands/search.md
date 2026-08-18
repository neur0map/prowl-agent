---
description: Answer a repository question from Prowl's cited index instead of grepping and reading many files.
argument-hint: [what you are looking for]
allowed-tools: Bash(prowl-agent:*)
---

Answer this repository question by querying Prowl's index with the read-only
`prowl-agent` CLI, not by grepping or reading whole files:

$ARGUMENTS

Before running anything, load and follow the bundled `code-search` skill. Its
routing table is the single source of truth for which `prowl-agent` command
answers which question, so choose the command from there -- do not work from a
table restated here.

Keep the boundary the skill defines: reserve native grep for an exact literal or
regex match and native glob for filename patterns; every semantic or structural
question is answered by a `prowl-agent` command. Report each answer with its
file:line citation.
