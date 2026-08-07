# Prowl refocus: analysis (2026-08-07)

This report records what changed when Prowl was refocused from a
"knowledge-native agent operating system" (CLI + web workbench + agent cockpit)
into a single, agent-first token-saving context engine, how it was verified, and
what still needs work. Everything below was measured on this machine against two
real repositories: `~/Work/inir` (a Quickshell rice, 1,413 files / 112,654
symbols) and `~/Work/ryoku-arch` (656 files / 11,262 symbols). Prowl was used in
read-only mode on both; only its gitignored `.prowl/` derived index was written.

## 1. The decision

The product now has one job: index a repository once, then let AI agents pull
bounded, cited, minimal context over the CLI and MCP instead of reading whole
files. The web UI, the durable-jobs/event-streaming plane, and the
Hermes-inspired agent-operations kernel were removed. They served an "agent
platform" ambition; the tool agents call is the whole product now.

## 2. Where we were vs where we are

| Metric | Before (f7f866c) | After (d2f4f89) | Delta |
|---|---|---|---|
| Go source files | 328 | 247 | -81 |
| Go lines of code | 54,005 | 34,334 | -19,671 (-36%) |
| Internal packages | 30 | 21 | -9 |
| Web frontend (src) | 5,492 LOC + built bundle | 0 | removed |
| Release binary size | 60.1 MB | 53.8 MB | -6.3 MB (-10.5%) |
| Test packages (all green) | 29 | 20 | leaner, still 100% pass |
| MCP `core` tool surface | n/a (UI-led) | 6 tools | schema/discovery cost down |

Roughly 25,000 lines were deleted (about 19.7k Go plus the 5.5k-line web app and
its bundle), with zero loss to the retrieval engine.

## 3. What was removed (and why it was safe)

Removal targets were chosen from the actual import graph, not by guesswork. The
MCP server and the `context` engine depend on none of the deleted packages.

- `web/` : the React workbench, its embedded dist bundle, and Playwright e2e.
- `internal/workbench` : the HTTP API/handlers that only served the UI.
- `internal/cli/open.go` and the `open` command : launched the web UI.
- `internal/jobs`, `internal/events` : durable jobs and SSE streaming, reachable
  only from `open`/`workbench`.
- `internal/{operations,agent,session,profile,toolruntime,entity}` and the
  `session` command : the agent-operations kernel. `toolruntime` and `entity`
  already had zero importers.

`internal/application` was trimmed (its workbench-only `OpenWorkbenchProject`,
startup-limit, and jobs wiring removed) but kept, because its `OpenProject` backs
every CLI and MCP path.

## 4. What the product is now

The retained engine: `index`, `query`, `context`, `knowledge`, `capability`,
`assist`, `boundedio`, `doctor`, `lsp`, `mcp`, `config`, `workspace`, `setup`,
`selfupdate`, and a trimmed `application`. Two surfaces:

- CLI query commands (`find`, `search`, `overview`, `context`, `impact`,
  `callers`, `callees`, `relations`, `hotspots`, `clusters`, `tests`,
  `entrypoints`, `references`, `changed`, `violations`) that any agent can shell
  out to with no server and no MCP tool-schema tokens.
- The MCP server (`serve`), verified to hand a new client a balanced surface:
  - `core` surface = 6 tools: `search_context`, `get_context`, `analyze_change`,
    `propose_knowledge_change`, `validate_knowledge`, `search_capabilities`.
  - 4 Resources: `prowl://workspaces`, and `.../current/{overview,changes,
    knowledge/index}`.
  - 5 Prompts: `understand-project`, `prepare-implementation`, `review-change`,
    `research-topic`, `update-knowledge`.

## 5. How the AI-agent experience improved

1. Lower discovery cost. An agent no longer sees a 23-tool grab bag up front; the
   `core` surface is 6 tools with progressive disclosure (`search_context` at
   `compact` mode, then `get_context` with an explicit token budget). Less schema
   in every request.
2. No stateful surface to babysit. No daemon, no browser, no session/job store.
   The agent shells a command or calls a tool and gets an answer.
3. Correctness on large repositories. Before this session the workbench could not
   even open a 112k-symbol project. Three defects were fixed (section 7) so the
   projections now serve large real repos in milliseconds.
4. Faster indexing. `inir` reindex went from stalling for minutes to 12.8s; a
   from-scratch index of `ryoku-arch` is 3.8s.

## 6. Token-saving evidence (the headline)

Measured with `prowl-agent status --json` (`saved = (baseline_bytes -
answer_bytes) / 4 * 0.7`, deliberately conservative) plus the manual
grep-and-read baseline from `docs/TOKENS.md`.

- inir, 8 realistic agent queries: `answer_tokens` 5,160, `saved_tokens`
  1,640,350. One question ("battery") answered by reading the 69 matching QML
  files is about 2.89 MB (about 723k tokens); Prowl's `find` answer is 3,057
  bytes (about 764 tokens). Roughly a 950x reduction on that question.
- ryoku-arch, cumulative: 841 queries, `answer_tokens` 932,137, `saved_tokens`
  52,682,866. A "keybind" lookup that naive grep-and-read would expand into about
  101 files returns a 629-byte answer.

The "million token saver" claim is not marketing here: a single repo crossed 1.6M
saved in 8 queries, and a well-used index shows 52M+.

## 7. Bugs found and fixed while dogfooding

All three were discovered by actually running the tool on `inir` and are covered
by the existing tagged test suite (plus one new regression test).

1. Overview refused any repo above 100k symbols (`brief`/`explore`/`timeline`
   returned HTTP 500). The refused counts are display-only capped scalars, so the
   ceilings were raised. (commit f7f866c)
2. `ResetDerived` deleted `symbols` with a self-referential `parent_id` cascade
   and no index, so a full reindex through the transaction path (which cannot
   disable foreign keys) ran O(n^2). On inir that was a multi-minute stall;
   adding `idx_symbols_parent` cut it to 12.8s. (commit f7f866c)
3. Timeline git parsing split on NUL but left git's inter-commit newline on the
   next commit hash, failing validation for any repo with more than one commit. A
   two-commit regression test now guards it. (commit f7f866c)

## 8. What is still failing or missing (evidence-backed backlog)

Dogfooding on QML-heavy repos exposed two real gaps. Neither is a quick, safe
patch; both are recorded here with evidence rather than hacked.

1. QML dependency resolution is weak, so `impact` and `callers` are near useless
   on QML. `impact modules/common/Config.qml` reports 0 dependents even though
   Config is a shared singleton used across the project; `callers` returns empty
   for local files. Cause: the QML parser only records `import <Module>`
   statements as edges (e.g. `QtQuick`, `Quickshell`, both unresolved external
   modules). It does not model QML's real coupling: local component use by type
   name (`BatteryIndicator { }` should edge to `BatteryIndicator.qml`), singleton
   references (`Config.foo`), or `import "relative/dir"`. Result: 2,354 of 6,035
   edges dangle on inir; 744 of 1,569 on ryoku-arch. Fix: extend the QML
   extractor to resolve type-name and singleton references to files. This is the
   single highest-value addition for QML/desktop-config users.

2. Context relevance is lexical-dominated when AI is off, so keyword-dense data
   files outrank code. Asking "how does the bar display battery status" on inir
   surfaces `translations/*.json` and docs above the actual `BatteryIndicator`
   components. `RankCandidates` scores on `LexicalScore` plus deterministic
   boosts; without a semantic reranker (optional Ollama), high-frequency locale
   files win. Fix, in order: (a) build the retrieval benchmark harness the old
   roadmap specified (question, expected sources, distractors, budget; measure
   precision, recall, utilization, and token cost together), then (b) add a
   query-aware down-weight for low-signal file classes (locale/i18n, generated,
   lockfiles) and validate against the benchmark. Shipping a ranking heuristic
   without the harness risks trading noise for different noise, so it is
   deliberately deferred to a measured change.

3. Smaller items observed: MCP `serve` opens the project with AI enabled, which
   adds a few seconds of startup on a stale index (a client should tolerate it,
   but a bounded startup would be nicer); and the generated setup files should be
   excluded from the index by default (already noted in the old roadmap Phase 0).

## 9. Verification performed

- `CGO_ENABLED=1 go build -tags sqlite_fts5 ./...` : clean.
- `CGO_ENABLED=1 go vet -tags sqlite_fts5 ./...` : clean.
- `CGO_ENABLED=1 go test -tags sqlite_fts5 ./...` : 20 packages, all pass.
- CLI: `open` and `session` are gone; all query commands, `serve`, `lsp`,
  `doctor`, `knowledge`, `context`, `capabilities` work on inir and ryoku-arch.
- MCP: `initialize` + `tools/list` + `resources/list` + `prompts/list` succeed;
  a real `find_symbol` call returned the correct component.
- Consolidation: `unstable` holds every branch's work; only `main` and `unstable`
  remain on origin. The refocus is commit d2f4f89 on `unstable`.
