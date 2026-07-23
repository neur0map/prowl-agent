# Product Evolution Handoff — 2026-07-23

## Continuation addendum

Work resumed after this checkpoint at the user's explicit request. The canonical plan is now being expanded from the Phase 3A knowledge workbench into a knowledge-native local agent operating system, based on revision-pinned behavioral research from NousResearch Hermes Agent.

Current contracts:

- Canonical roadmap: `docs/plans/2026-07-23-prowl-agent-product-evolution.md`
- Phase 3A execution plan: `docs/plans/2026-07-23-phase-3a-workbench-completion.md`
- Detailed clean-room port plan and parity ledger: `docs/plans/2026-07-23-hermes-inspired-agent-operations-port.md`
- Upstream evidence pin: `NousResearch/hermes-agent@c4f5a45d5d9903998fb318ac6f3c5e6623e60445`

Execution order: Phase 3A real-data workbench completion → Phase 3B provider-neutral agent kernel → Phase 3C durable Kanban/live operations → original Phases 4–6 → selected broader parity in Phase 7.

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

Phase 0 remains open for a narrower reason: no pull request has exercised the branch workflow, and the green Actions history on `main` covers the older `build-binary` definition rather than the Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 matrix. Do not claim Phase 0 complete until a PR or equivalent authorized run executes the tracked branch workflow and the job/artifact/smoke evidence is recorded. `/tmp/prowl-release-workflow.yml` is only a stale preparation artifact and is no longer authoritative.

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
- 32-byte cryptographically random, unpadded URL-safe bearer token
- Token passed to the browser only in the URL fragment
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

## First actions when resuming

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

- Decide whether to keep bearer-only requests for all operations or introduce a session-cookie/CSRF exchange. Bearer-only avoids cookie CSRF and already confines the token in memory; if cookies are introduced later, mutations must require strict CSRF protection.

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

After Phase 3 is independently complete:

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
