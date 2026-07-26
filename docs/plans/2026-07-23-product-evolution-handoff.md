# Product Evolution Handoff — 2026-07-23

## Final stop handoff — 2026-07-24

This section is the authoritative resume point. Carlos explicitly requested: commit and push everything, record the exact state and remaining work, then stop. Do not resume implementation from older sections of this file.

### Repository checkpoint

- Repository: `/home/neur0map/workspace/prowl-agent`
- Branch: `product-evolution`
- Implementation checkpoint: `61bd515 feat(workbench): add bounded project-backed startup`
- Earlier local checkpoints included in the branch:
  - `1c62e67 feat(application): make project generations atomic`
  - `807f6f2 docs(plan): finalize workbench and operations roadmap`
- Last remote checkpoint before this stop operation: `f45b769 docs(plan): add Hermes-inspired agent operations roadmap`
- The handoff itself is committed immediately after `61bd515`; use `git log -3 --oneline` after pulling to identify its final SHA.
- Draft PR: <https://github.com/neur0map/prowl-agent/pull/1>
- Live toolchain at stop: `go version go1.26.4 linux/amd64`; `go.mod` also declares Go 1.26.4.
- SQLite tests on this host require `CGO_ENABLED=1` and `-tags sqlite_fts5`.

### Canonical plans

- Product roadmap: `docs/plans/2026-07-23-prowl-agent-product-evolution.md`
- Phase 3A execution contract: `docs/plans/2026-07-23-phase-3a-workbench-completion.md`
- Hermes clean-room adaptation and parity ledger: `docs/plans/2026-07-23-hermes-inspired-agent-operations-port.md`
- This handoff: `docs/plans/2026-07-23-product-evolution-handoff.md`
- Immutable Hermes research pin: `NousResearch/hermes-agent@c4f5a45d5d9903998fb318ac6f3c5e6623e60445`

### What is complete

#### Phases 1–2

Phase 1 durable Markdown/OKF knowledge and Phase 2 unified retrieval/context/MCP foundations remain complete, independently accepted, committed, and pushed. Their detailed evidence is preserved below.

#### Phase 3A A1

A1 is complete, independently accepted, and committed as `1c62e67`:

- one deterministic `application.Project` lifecycle across CLI, MCP, LSP, query, context, setup, and restart paths;
- strict configuration and freshness behavior;
- cross-process refresh serialization;
- generation-atomic structural/vector publication and complete logical reads;
- interrupted-generation repair and source-set validation;
- cancellable inherited stdin for LSP with joined shutdown ordering.

#### Phase 3A A2

A2 was independently accepted and is included in `61bd515`:

- real project-backed `GET /api/v1/health` and `GET /api/v1/brief`;
- stable bounded JSON success/error envelopes and request IDs;
- generation-pinned Brief/Health reads;
- canonical validated `meta.resource_version` handling;
- SQL-level bounded Overview reads and typed bounded-work errors;
- bounded knowledge projection using one pinned `os.Root`;
- strict path, identifier, timestamp, UTF-8, control-character, and response-size validation;
- request cancellation reaches SQL and knowledge traversal;
- missing service fails closed with an enveloped `503`.

A later defensive A3 review found one real A2 defect: context-aware knowledge reads could block opening a Markdown FIFO. `61bd515` includes the regression and repair using rooted nonblocking descriptor validation plus context-aware bounded reads.

#### Phase 3A A3 implementation

A3 implementation and all known remediations are included in `61bd515`:

- exact default startup limits: 250 ms and 2,000 nonignored candidate paths;
- candidate 2,001 rejected before candidate inspection/read/hash;
- incremental directory reads rather than unbounded `WalkDir` materialization;
- one pinned accepted `os.Root` for bounded recursion, nested `.gitignore`, metadata, and source content;
- rooted nonblocking descriptor opens with post-open regular-file validation;
- FIFO config/source inputs cannot strand startup;
- context-aware config and SQLite open/schema/migration/metadata paths;
- SQLite BUSY/LOCKED at the startup bound classifies as `startup_refresh_required`;
- bounded workspace resolution while ordinary `workspace.Resolve` and `OpenProject` stay synchronous;
- canonical bounded signatures for unchanged ordinary files and in-root file symlinks;
- current projects open without refresh; stale/deadline/candidate-overflow startup refuses before listen;
- real `workbench.Service` is assembled before listener creation;
- project/listener cleanup is close-once with joined errors.

### A3 acceptance status: not yet final

Do **not** call A3 accepted solely from green tests.

The first remediated same-tree review split:

- specification reviewer: `PASS`;
- defensive reviewer: `REQUEST_CHANGES` for:
  1. canonical-vs-bounded signature mismatch for in-root source symlinks;
  2. blocking FIFO open in context-aware knowledge reads;
  3. a residual context-unaware workspace-resolution metadata seam.

All three were repaired regression-first before `61bd515`. Exact reports:

```text
/home/neur0map/.hermes/cache/delegation/subagent-summary-0-20260724_020445_923022.txt
/home/neur0map/.hermes/cache/delegation/subagent-summary-1-20260724_020445_923166.txt
```

A fresh two-reviewer batch was dispatched against the final implementation bytes:

```text
delegation: deleg_2f1f2620
roles: A3 specification acceptance + defensive correctness/security
status at stop: running / result not yet consumed
```

The commit operation did not alter reviewed implementation bytes; it only recorded them. This handoff documentation was added afterward. On resume, read the complete `deleg_2f1f2620` reports first. A completion notification alone is not approval. If either report finds a blocker/high issue, reproduce it and repair regression-first. If both approve, record A3 accepted and continue to A4.

### Verification on the `61bd515` implementation

The following passed after the final symlink, FIFO-knowledge, and workspace-resolution fixes:

```sh
export PATH="$HOME/sdk/go/bin:$PATH" CGO_ENABLED=1

go test -race -tags sqlite_fts5 \
  ./internal/cli ./internal/application ./internal/config ./internal/index \
  ./internal/store ./internal/workspace ./internal/knowledge \
  -run 'Test(OpenCommand|StartupFreshness|OpenProjectClose|LoadContext|OpenContext|ResolveContext|RepositoryListContextBoundedRejectsFIFO|.*Candidate)' \
  -count=1

go test -tags sqlite_fts5 ./... -count=1
go test -race -tags sqlite_fts5 ./... -count=1
go vet -tags sqlite_fts5 ./...
go mod verify
go mod tidy -diff
git diff --check
```

Observed:

- exact selector membership was verified with `go test -list`;
- canonical A3 race selector: pass;
- full tagged repository suite: pass;
- full tagged repository race suite: pass;
- vet: pass;
- module verification: `all modules verified`;
- tidy diff: clean;
- diff check: clean;
- symlink parity and immediate bounded-open regressions: pass 25 times under race;
- FIFO knowledge no-writer regression: pass 25 times under race;
- SQLite lock regression: pass 20 times under race;
- application contention regression: pass 10 times under race.

An earlier compiled-binary probe returned authenticated HTTP 200, `api_version: "v1"`, health `status: "ok"`, and a canonical resource version. It predates the final symlink/knowledge-only repairs, so rerun compiled acceptance at A5 rather than treating it as final current-binary proof.

### Frontend state and known gate

- Frontend unit tests and production build passed before the latest Go-only repairs.
- `web/dist` was not regenerated by A2/A3.
- Current Playwright fixture is known to build its test binary under the indexed repository's `.tmp/...` path after indexing. That mutates the generation and A3 correctly returns `startup_refresh_required` before serving.
- Fix the fixture by building outside the indexed workspace as part of A5 compiled-binary acceptance; do not weaken A3 freshness to make the faulty fixture pass.
- This known fixture failure means `npm run test:e2e` is not presently a green final gate.

### A4 design ready, implementation not started

A read-only A4 design audit is saved at:

```text
/home/neur0map/.hermes/cache/delegation/subagent-summary-0-20260724_020344_334332.txt
```

The Phase 3A plan now explicitly allows A4 to modify `internal/workbench/handler_test.go`; preserving static `APIOptions.Token` compatibility would retain the vulnerability A4 must remove.

A4 must implement, with strict RED/GREEN tests:

- separate 256-bit launch nonce and bearer;
- 60-second, exact-origin, atomically single-use `POST /api/v1/auth/bootstrap`;
- nonce invalidated before bearer response;
- nonce and bearer stored as digests server-side;
- fragment removed synchronously before frontend exchange;
- bearer held only in browser module memory;
- authenticated fetch restricted to normalized final same-origin `/api/v1/` paths;
- `redirect: "error"` for authorization-bearing fetches;
- redacted automatic launch output;
- `0700` generated directory and exclusive `0600` no-browser handoff file;
- cleanup on consume, expiry, shutdown, and setup failure;
- explicit reveal only when stdin and stderr are real TTYs;
- no legacy static-token fallback.

A4 must not claim compiled-binary security until A5 regenerates and verifies `web/dist`.

### What remains

Execute in the order defined by the canonical plans:

1. Consume `deleg_2f1f2620`; close any A3 blocker/high finding or record dual approval.
2. Phase 3A A4: nonce-to-bearer browser bootstrap.
3. A5: asynchronous frontend bootstrap, Home/Brief views, regenerate `web/dist`, repair Playwright fixture, and compiled-binary vertical acceptance.
4. B1–B7c: real Explore, Context Lens, Impact, Knowledge, Timeline, Setup, bounded source previews, and frontend hardening.
5. C1–C5: durable jobs, SSE/reconnect, cancellation, export, activity/idle behavior, accessibility, localization, fixtures, and guided journeys.
6. D1–D2: compiled acceptance, security/context/comprehension oracles, and independent Phase 3A closure.
7. Phase 3B: provider-neutral agent kernel—profiles, sessions, prompt/context assembly, tools/toolsets/skills, approvals, steering, checkpoints, accounting, and delegation.
8. Phase 3C: durable Kanban/live operations—dependencies, transactional transitions, claims/leases/heartbeats, stale recovery, retries, cancellation, evidence/reviews, workers, append-only events, cursors/reconnect, and operations UI.
9. Phase 4: bridge prerequisite and Obsidian integration.
10. Phase 5: memory, recovery, research, automation, cron/webhooks, privacy, retention, and gardening.
11. Phase 6: traceability, multilingual maturity, adapters, plugin SDK/catalog, and scoped workspace ecosystem.
12. Phase 7: selected broader Hermes parity and management surfaces.
13. Full integration/security/concurrency/performance/migration/browser/plugin review and requirement-by-requirement final audit.
14. Phase 0 remains open until the five-target release workflow executes from an eligible GitHub branch/default-branch context and its artifacts/smoke evidence are recorded.

### First resume procedure

```sh
cd /home/neur0map/workspace/prowl-agent
export PATH="$HOME/sdk/go/bin:$PATH" CGO_ENABLED=1

git switch product-evolution
git pull --ff-only origin product-evolution
git status --short --branch
git log -5 --oneline --decorate
```

Then:

1. Read this final stop section and the three canonical plans.
2. Read both complete `deleg_2f1f2620` reports and verify which exact tree they inspected.
3. Confirm the branch is clean and remote/local SHAs match.
4. If A3 has dual approval, begin A4 with the strict RED tests from the A4 design report. Otherwise repair confirmed findings before A4.
5. Continue autonomous implementation only after a new explicit user instruction; this handoff records a deliberate stop.

## Continuation addendum

Work resumed after this checkpoint at the user's explicit request. The canonical plan is now being expanded from the Phase 3A knowledge workbench into a knowledge-native local agent operating system, based on revision-pinned behavioral research from NousResearch Hermes Agent.

Current contracts:

- Canonical roadmap: `docs/plans/2026-07-23-prowl-agent-product-evolution.md`
- Phase 3A execution plan: `docs/plans/2026-07-23-phase-3a-workbench-completion.md`
- Detailed clean-room port plan and parity ledger: `docs/plans/2026-07-23-hermes-inspired-agent-operations-port.md`
- Upstream evidence pin: `NousResearch/hermes-agent@c4f5a45d5d9903998fb318ac6f3c5e6623e60445`

Current execution order: Phase 3A A1–D2 real-data/security completion → bounded Phase 3B0 B0.1–B0.6d → bounded Phase 3C0 C0.1–C0.5c → Phase 3B/3C breadth → Phase 3C1 selected pinned Kanban behavior → Phase 4 bridge prerequisite and Obsidian → Phases 5–7.

This file still records the exact `95ba94f` tracer-bullet checkpoint, verification, and review caveat. It is historical evidence, not the current end-of-work handoff.

## Resume point at the historical checkpoint

- Repository: `/home/neur0map/workspace/prowl-agent`
- Branch: `product-evolution`
- Phase 2 parent commit: `098f249206115de3e4b91749d4b991e7183b681b`
- Phase 2 status: complete, independently reviewed, committed, and pushed
- Phase 3 status at checkpoint: **in progress; first secured workbench tracer bullet committed with this handoff**
- Canonical contract: `docs/plans/2026-07-23-prowl-agent-product-evolution.md`
- Go environment:

  ```sh
  export PATH="$HOME/sdk/go/bin:$PATH"
  export CGO_ENABLED=1
  ```

- SQLite tests on this host require `-tags sqlite_fts5`.

The former stop directive was satisfied by commit `95ba94f`; this continuation is authorized by the user's follow-up request.

## Completed before this checkpoint

### Phase 0

Most Phase 0 implementation is complete. Follow-up inspection confirmed that the five-target `.github/workflows/release.yml` is already committed and identical on `origin/product-evolution`; the repository account has `ADMIN` permission. The old authorization blocker is obsolete.

Phase 0 remains open for a narrower reason: no pull request has exercised the branch workflow, and the green Actions history on `main` covers the older `build-binary` definition rather than the Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 matrix. A manual dispatch of the default `build-binary` workflow against `95ba94f` succeeded at build, test, and checksum in [run 30040678190](https://github.com/neur0map/prowl-agent/actions/runs/30040678190), with release publication correctly skipped; this is useful Linux branch sanity evidence but is not matrix evidence. GitHub would not load the expanded pull-request workflow from a non-default base branch, so the full matrix can run only after that workflow reaches `main` (or the default branch is deliberately changed).

Do not claim Phase 0 complete until the tracked branch workflow executes after landing and the job/artifact/smoke evidence is recorded. `/tmp/prowl-release-workflow.yml` is only a stale preparation artifact and is no longer authoritative.

### Phase 1

Complete and pushed:

- Canonical Git-visible Markdown knowledge under `.prowl/knowledge/`
- OKF v0.1 interchange
- Proposal review lifecycle
- Source anchors and freshness
- Conflict-safe acceptance and rooted rollback
- Linting, projection, migration, backup, and rollback

### Phase 2

Complete and pushed as commit `098f249`:

- `prowl.context/v1` packets
- Budgeted lexical, knowledge, and SQL-bounded direct one-hop graph retrieval
- Optional production semantic reranking with deterministic fallback
- Privacy-safe HMAC-linked aggregate traces and 30-day retention
- Capability discovery
- MCP `legacy`, `core`, and `all` surfaces
- Resources, Prompts, resource links, optional sampling, and fail-closed elicitation
- Rooted knowledge/source protections and proposal rollback reporting
- Instrumented six-case retrieval evaluation and benchmark
- Legacy 17-tool compatibility descriptor pin

The exact Phase 2 staged snapshot received an independent PASS before commit.

## Phase 3 checkpoint committed here

This checkpoint is a deliberately small vertical slice proving:

```text
Node source → tested Preact shell → deterministic Vite bundle → Go embed →
secured loopback HTTP server → prowl-agent open → real Chromium + authenticated API
```

### Go and CLI

- New visible command:

  ```sh
  prowl-agent open
  prowl-agent open --no-browser
  prowl-agent open --port 43117
  ```

- IPv4 loopback-only listener using `tcp4` and `127.0.0.1`
- Kernel-selected free port by default
- **Historical implementation only:** 32-byte cryptographically random bearer placed in the browser fragment. This is superseded by Phase 3A A4: a 60-second single-use fragment nonce is atomically exchanged for a distinct memory-only API bearer.
- Cross-platform browser launch using argv-safe `exec.Command`
- Browser launcher is reaped asynchronously with `Wait`; a regression test uses a real short-lived child process
- Ctrl-C/SIGTERM cancellation and bounded HTTP shutdown
- Static shell remains unauthenticated because it contains no project data or token
- `/api/v1/*` remains bearer protected
- Exact `Host` enforcement
- Exact non-empty browser `Origin` enforcement
- `Sec-Fetch-Site: cross-site` rejection
- No permissive CORS headers
- Constant-time bearer comparison
- CSP, `Referrer-Policy`, `X-Content-Type-Options`, and cache controls
- HTML/API use `no-store`; generated hashed assets use immutable caching

### Frontend

- Preact + TypeScript + Vite
- Exact npm dependency pins and lockfile
- Committed `web/dist` so ordinary Go builds do not require Node
- Go `embed.FS` contract test
- Accessible shell with primary links for:
  - Home
  - Explore
  - Context Lens
  - Impact
  - Knowledge
  - Timeline
  - Setup
- Semantic landmarks, headings, lists, skip link, visible focus, responsive layout, and reduced-motion CSS
- URL fragment token is removed immediately with `history.replaceState`
- Bearer remains in module memory only
- No cookie, local storage, session storage, or token query persistence
- `apiFetch` refuses absolute, protocol-relative, non-v1, cross-origin, or fragment-bearing destinations before attaching Authorization
- Deterministic contrast assertion for the smallest sidebar label
- Axe gate fails on both violations and serious incomplete findings
- Real keyboard assertion for skip-link focus and activation

### Tests and build gates

- Vitest owns only `web/src/**/*.test.{ts,tsx}`
- Playwright owns only `web/e2e`
- `npm run check` runs strict typecheck, Vitest, production build, then Playwright
- Playwright:
  - builds the actual tagged Go binary
  - starts `prowl-agent open --no-browser --port 0`
  - loads embedded assets in Chromium
  - verifies the token fragment is removed
  - verifies no browser persistence
  - calls the real authenticated health API
  - checks keyboard behavior
  - runs axe
  - terminates the Go process in `finally`
- `npm audit --audit-level=low` reports zero vulnerabilities
- A clean `npm ci` reproduced the pre-fix bundle byte-for-byte; rebuild determinism should be reconfirmed for this final committed bundle when work resumes

## Independent review history for this checkpoint

The first exact staged candidate hash was:

```text
20f21e033cc8bdc866cfab7383513a0f1b5a96a109c558834c6fc2dee826413f
```

Two independent reviewers returned FAIL:

1. `openBrowserURL` called `Start` without `Wait`, leaving a confirmed zombie launcher while the workbench remained active.
2. Axe's zero-violation list masked serious incomplete contrast results; `.privacy-note` was measured at 3.30:1. The test also had an unsupported `aria-label` on a plain `div` and decorative glyph ambiguity.

Both blockers were fixed before this commit:

- Browser children now receive asynchronous buffered `Wait`, with a real child-process regression test.
- Unresolvable gradient/backdrop surfaces were replaced with solid surfaces.
- The smallest visible text now measures at least 4.5:1; the explicit target is approximately 5.86:1.
- Invalid ARIA was removed.
- Decorative text glyphs were replaced with CSS decoration.
- Playwright now fails on serious axe incompletes as well as violations.

**Important:** the post-fix snapshot did not receive another independent subagent PASS before the user's stop request. All direct tests passed, but a fresh independent review remains the first resumption gate.

Review reports:

```text
/home/neur0map/.hermes/cache/delegation/subagent-summary-0-20260723_181716_555155.txt
/home/neur0map/.hermes/cache/delegation/subagent-summary-1-20260723_181716_555278.txt
```

## Verification state at stop

Passed after the frontend accessibility and transport fixes:

```sh
cd web
npm audit --audit-level=low
npm run check
```

Observed:

- npm audit: 0 vulnerabilities
- TypeScript: pass
- Vitest: 4 tests pass
- Vite production build: pass
- Real compiled-binary Chromium test: pass
- Axe violations: none
- Serious axe incompletes: none
- Explicit small-text contrast: at least 4.5:1

Passed after browser-process fix:

```sh
go test -tags sqlite_fts5 ./internal/cli \
  -run 'TestStartAndReap|TestBrowserLauncherHelper|TestOpenCommand' -count=1
```

Before the two review-driven fixes, the full tagged Go suite, tagged vet, and focused race suite passed. The final commit procedure reruns those gates; consult the commit/session output rather than assuming success.

## Historical first actions (superseded)

The commands below record the `95ba94f` checkpoint procedure only. **Do not treat them as the current resume procedure.** The final section of this file is the sole current instruction.

1. Read this handoff and the Phase 3 section of the canonical plan.
2. Confirm the branch and clean tree:

   ```sh
   git branch --show-current
   git status --short
   git log -1 --oneline
   ```

3. Run the exact current gates:

   ```sh
   export PATH="$HOME/sdk/go/bin:$PATH"
   export CGO_ENABLED=1

   go test -tags sqlite_fts5 ./... -count=1
   go vet -tags sqlite_fts5 ./...
   go test -race -tags sqlite_fts5 \
     ./internal/workbench ./internal/cli ./web -count=1

   cd web
   npm ci
   npm audit --audit-level=low
   npm run check
   cd ..

   git diff --exit-code -- web/dist
   git diff --check
   ```

4. Dispatch a fresh independent blocker review of the committed checkpoint, focused on:
   - child-process reaping
   - host/origin/authentication behavior
   - shutdown races
   - serious axe incomplete handling
   - deterministic text contrast
   - token destination confinement
   - committed bundle reproducibility
5. Fix any blocker and commit it separately before adding Phase 3 product data endpoints.

## Open design decisions before expanding the API

- Standardize the general API envelope. The current health response uses:

  ```json
  {"api_version":"v1","status":"ok"}
  ```

  The architecture review recommended a stable `schema_version: "prowl.api/v1"` envelope for non-context endpoints. Resolve this before many handlers depend on the current shape. Context endpoints must return canonical `prowl.context/v1` packets directly.

- Historical open question, now resolved: use A4's single-use nonce-to-memory-bearer exchange; do not introduce cookies in Phase 3A. Any future cookie mode requires a separate CSRF design and migration.

- Extract shared project/service assembly before duplicating CLI, MCP, and web construction logic.

## Phase 3 work remaining

The current shell is a tracer bullet, not the completed workbench. Remaining contract work includes:

1. Shared project service assembly, incremental freshness, and clean store lifecycle.
2. Stable API v1 error and response contracts with request-body/query limits.
3. Real Home/Brief aggregation from overview, status, doctor, Git, knowledge, and review inbox.
4. Rooted, bounded, symlink-safe source-range previews extracted from existing MCP protections.
5. Progressive Explore hierarchy: project → domain → flow → file/symbol/concept.
6. Deterministic guided tours with 5–12 steps and source evidence.
7. Three diverse fixture projects:
   - layered Go authentication service
   - TypeScript checkout monorepo
   - desktop/rice configuration project
8. Context Lens calling the same `context.Service`, with parity against CLI/MCP after excluding only per-execution trace IDs.
9. Impact composition from blast radius, callers/references, tests, Git diffs, and actual knowledge anchors.
10. Knowledge detail/backlinks and review inbox diff/accept/reject UI; preserve conflict and rollback semantics.
11. Timeline service merging bounded Git, knowledge-log, and privacy-safe context trace events. Do not fabricate Phase 5 observations.
12. Setup detect → plan → preview → apply → verify, after moving setup logic out of package `cli`.
13. Context-aware indexing jobs, SSE progress, bounded event buffers, reconnect, cancellation, and disconnect cleanup.
14. Workbench activity tracking and configurable idle shutdown that does not fire during active jobs, requests, or streams.
15. Dependency-free message catalogs, English and `en-XA` pseudolocale, and hardcoded-string checks.
16. Broader keyboard, screen-reader, focus restoration, responsive, and reduced-motion tests.
17. Deterministic visual regression snapshots at desktop and narrow widths.
18. Read-only standalone single-file HTML export with provenance and offline verification.
19. Three fixture-driven complete guided journeys and newcomer-comprehension exit evidence.
20. Final independent Phase 3 review, requirement-by-requirement plan audit, coherent commit sequence, and push.

## Later phases

Historical ordering only; superseded by the final current procedure:

- Phase 4: Obsidian plugin and stdio bridge
- Phase 5: episodic memory adapters and research facade/packs
- Phase 6: spec traceability, SDKs/adapters, multilingual support, registry/catalog
- Integration review across all surfaces
- Final line-by-line plan audit

## Constraints to preserve

- Canonical accepted knowledge remains Markdown under `.prowl/knowledge/`; SQLite is derived.
- No prompts, source bodies, snippets, provider payloads, credentials, or sampled responses in aggregate traces.
- Optional AI failure must preserve deterministic local behavior.
- Knowledge writes remain proposal-only and human reviewed.
- Root confinement, bounded reads, symlink resistance, conflict checks, and observable rollback failures are non-negotiable.
- `legacy` remains the default MCP surface and its 17 descriptors remain compatible.
- The tracked five-target release workflow is already on `origin/product-evolution`; do not rewrite it without a source-backed need. Phase 0 requires real branch/PR Actions evidence before completion.
- Commits use concise human-written messages with no AI/co-author attribution.

## Current resume procedure (supersedes every earlier directive)

This is the only operational resume procedure. Earlier checkpoint commands and “later phases” text remain historical evidence.

1. Work in `/home/neur0map/workspace/prowl-agent` on `product-evolution`. Read the full current versions of the canonical product plan, Phase 3A execution plan, Hermes-port plan, and this final section before editing.
2. Preserve existing work. Run `git status --short`, `git diff --name-only`, and `git diff --check`; do not reset the later Phase 0 CI-evidence correction in this file or any in-progress A1 files. At the time this procedure was written, A1 work existed in `internal/application/**`, `internal/cli/{init,query,context,serve,lsp,restart,freshness}.go` and tests, `internal/index/{index,walk,watch,embed}.go` and tests, `internal/store/vectors.go`, and the Go module lock dependency; re-inspect rather than assuming it is complete.
3. Resume **Phase 3A A1** at its independent-review gate. Init, query CLI, context CLI, MCP serve, LSP, and restart refresh must all use `application.Project`; malformed configuration must fail consistently; interrupted/changed-during-refresh generations must fail closed and repair; local and cross-process lock waits must honor cancellation; watcher callbacks must be joined before project close; vector completeness must be model-scoped. A1 starts no goroutines. Run A1's focused command and record observed RED/GREEN/review output before advancing. **A3**, not A1, owns the 250 ms/2,000-path workbench startup probe and typed pre-listen/deferred-refresh behavior.
4. Execute Phase 3A strictly by IDs **A2 → A3 → A4 → A5 → B1–B6 → B7a–B7c → C1–C5 → D1–D2**. Follow each task's exact file/route/DTO/migration ownership and seam serialization. A4's nonce-to-bearer bootstrap, C1–C2's scoped `project-job` outbox cursor/store, and D2's executable security/context/comprehension oracles are blocking gates.
5. After D2 passes and receives independent security/requirements PASS, execute only Hermes-port **B0.1–B0.6d**, then **C0.1–C0.5c**. Run each table gate and the compiled-binary kill/restart oracles. Do not pull FTS, repair breadth, external lanes, broad attachments/subscriptions, provider breadth, automation breadth, or management UI into these tracer slices.
6. Only after both tracer oracles pass may Phase 3B/3C breadth begin. Then execute Phase 3C1 HP-068/069/071–074; HP-070 swarm remains deferred. Before any Obsidian feature, pass the exact Phase 4 `internal/bridge/**` stdio v1 compatibility gate. TUI/broad ACP remain Phase 7.
7. At every checkpoint run the focused task gate, then the plan's full gate where required. Before handoff run:

   ```sh
   export PATH="$HOME/sdk/go/bin:$PATH" CGO_ENABLED=1
   go test -tags sqlite_fts5 ./... -count=1
   go vet -tags sqlite_fts5 ./...
   cd web
   npm ci
   npm audit --audit-level=low
   npm run check
   cd ..
   git diff --exit-code -- web/dist
   git diff --check
   ```

8. Update the relevant verification ledger with exact command output, fixture, excluded volatile fields, thresholds/sample evidence, changed paths, migration/rollback evidence, and independent review disposition. Never claim phase completion from subjective prose, mock-only tests, or a route count.

## Live resume checkpoint - 2026-07-26

This section is the authoritative live resume point and supersedes the stale
state assumptions in steps 1-5 above. The architecture and execution-order
contracts in those steps remain binding.

Repository state:

- Active isolated worktree:
  `/home/nero/Work/prowl-agent/.worktrees/prowl-full-evolution`
- Branch: `prowl-full-evolution`
- Phase 3A final implementation/security checkpoint: `84499f4`
- Phase 3A acceptance-evidence checkpoint: `ec4fa76`
- Worktree was clean immediately after `ec4fa76`

Phase 3A A1-D2 is complete and accepted. The final controller gate passed 25
tagged Go packages plus 2 without tests, vet, the 6-package race suite, locked
dependency install/audit, 102 frontend tests, production build, 13 compiled
Chromium tests, bundle parity, and whitespace. Three independent participants
scored 40/45 overall; every fixture journey scored at least 4/5 and completed
in under 36 seconds. `ReviewD2` returned requirements/security PASS and
`SecurityD2Final` returned a scoped security-fix re-review PASS. The disclosed
visible browser-relay deviation and all 45 anonymized rows are recorded in
`docs/verification/phase-3a-requirements.md`.

The next exact task is Hermes-port **B0.1**, followed strictly by
**B0.2 -> B0.3 -> B0.4 -> B0.5 -> B0.6a -> B0.6b -> B0.6c -> B0.6d**, then
**C0.1 -> C0.2 -> C0.3 -> C0.4 -> C0.5a -> C0.5b -> C0.5c**. Use
`docs/plans/2026-07-23-hermes-inspired-agent-operations-port.md` as the
canonical implementation/parity contract and the existing SDD briefs under
`.superpowers/sdd/2026-07-23-hermes-inspired-agent-operations-port/` as
task-local execution records. Do not begin Phase 3B/3C breadth until both
tracer-slice compiled kill/restart oracles pass.
