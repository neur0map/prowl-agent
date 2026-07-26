# Phase 3A Requirements Evidence

## Acceptance status and evidence boundary

This file is the automated D2 checkpoint for the tree based on accepted D1 commit
`43e4f3f`. It is not a declaration that Phase 3A is accepted. Historical entries
below are committed observations copied from `full-roadmap-ledger.md`, the Phase
3A progress ledger, and the accepted D1 report. “Final D2” means a command was
observed on the D2 checkpoint worktree; it does not promote a historical command
to final-tree evidence.

The following acceptance evidence is deliberately **PENDING**:

- three independent newcomer participants and all 45 binary-scored answers;
- independent security review of the committed D2 checkpoint;
- independent requirements review of the committed D2 checkpoint;
- the controller-owned project-wide Go, race, vet, npm, bundle-diff, and
  whitespace gates listed under “Remaining controller gates”.

No participant score or independent-review disposition is inferred from an
automated test, route count, implementer self-review, or historical review.

## Evidence command catalog

### Historical committed observations

| ID | Task evidence | Exact observed command | Observed result |
| --- | --- | --- | --- |
| H-A1 | Shared project/startup baseline | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/cli ./internal/application ./internal/config ./internal/index ./internal/store ./internal/workspace ./internal/knowledge -run 'Test(OpenCommand\|StartupFreshness\|OpenProjectClose\|LoadContext\|OpenContext\|ResolveContext\|RepositoryListContextBoundedRejectsFIFO\|.*Candidate)' -count=1` | Passed, 7 packages. A1–A3 before-branch history is inherited from the canonical handoff. |
| H-A2 | Brief/API bounded projection | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/workbench ./internal/query ./internal/store ./internal/knowledge -run 'Test(Brief\|API\|Projection\|MalformedResourceVersion\|IdentifierValidation\|SuccessWriter\|ErrorWriter\|Overview\|RepositoryList\|RepositoryRootSwap)' -count=1` | Passed, 4 packages. |
| H-A4 | Bootstrap backend/launcher | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/workbench ./internal/cli -run 'Test(Bootstrap\|Nonce\|OpenOutput\|BrowserLaunch\|OpenCommand\|WriteBootstrapHandoff)' -count=1` | Passed, 2 packages. |
| H-A4W | Bootstrap frontend | `cd web && npm test -- --run src/transport/auth.test.ts src/transport/api.test.ts` | Passed, 4 tests. |
| H-A5 | Compiled Brief vertical slice | `cd web && npm run check` | Passed: 9 unit tests, production bundle, and 1 compiled-binary Playwright slice. |
| H-MA | Milestone A parity/bootstrap | `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/application ./internal/cli ./internal/mcp ./internal/workbench -run 'TestCLI_MCP_WorkbenchProjectParity\|TestBootstrap' -count=1` | Passed, 4 packages. |
| H-B1 | Explore/tour/source | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/workbench -run 'Test(Explore\|GuidedTour\|SourcePreview)' -count=1` | Passed, 1 package. |
| H-B2 | Canonical Context Lens | `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/context ./internal/cli ./internal/mcp ./internal/workbench -run 'TestCanonicalContextProjection\|TestContextLensParity' -count=1` | Passed, 4 packages. |
| H-B3 | Impact/Knowledge/Timeline | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/workbench -run 'Test(Impact\|KnowledgeRead\|Timeline)' -count=1` | Passed, 1 package. |
| H-B4 | Knowledge decisions | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/knowledge ./internal/cli ./internal/workbench -run 'Test(ProposalAPI\|ExpectedVersion\|Rollback\|Idempot\|Knowledge)' -count=1` | Passed, 3 packages. |
| H-B5 | Setup mutations | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/setup ./internal/cli ./internal/workbench -run 'TestSetup(Detect\|Plan\|Apply\|Verify\|Conflict\|Rollback\|Audit)' -count=1` | Passed, 3 packages. |
| H-B6 | Route inventory/mutation integration | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/workbench -run 'Test(RouteInventory\|RequestBounds\|PrincipalDerivation\|MutationAuth)' -count=1` | Passed, 1 package; compiled setup-route smoke also returned bootstrap 200 and authenticated detect 200. |
| H-B7A | Read-only analysis views | `cd web && npm test -- --run src/features/explore src/features/context src/features/impact && npm run typecheck` | Passed, 3 files / 21 tests; TypeScript clean. |
| H-B7B | Knowledge/timeline/setup views | `cd web && npm test -- --run src/features/knowledge src/features/timeline src/features/setup && npm run typecheck` | Passed, 3 files / 34 tests; TypeScript clean. |
| H-B7C | Production transport/shell | `cd web && npm test -- --run src/features src/transport/api.test.ts src/app/App.test.tsx && npm run build` | Passed, 9 files / 78 tests; production bundle built. |
| H-C1 | Cursor/broker conformance | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/events -run 'Test(CursorScope\|AdapterConformance\|ConnectedSubscriberSweep\|RetentionReset\|SlowSubscriber)' -count=1` | Passed, 1 package. |
| H-C2 | Durable jobs | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/jobs ./internal/events ./internal/application ./internal/index ./internal/cli -run 'Test(Job\|JobsDBPath\|OutboxTransaction\|CommitBeforePublish\|PublisherWatermark\|StartupRefreshJob\|OpenStartupJobWiring\|Cancel\|Restart\|IndexWithProgress\|Migration\|Concurrent\|WorkerActive)' -count=1` | Passed, 5 packages; affected application/CLI race regression also passed historically. |
| H-C3 | SSE/job routes | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/workbench ./internal/events ./internal/jobs -run 'Test(SSE\|JobRoutes\|Reconnect\|Shutdown\|Redaction)' -count=1` | Passed, 3 packages. |
| H-C4 | Live frontend | `cd web && npm test -- --run src/transport/events.test.ts src/features/jobs/JobStatus.test.tsx src/app/App.test.tsx && npm run build` | Passed, 3 files / 30 tests; TypeScript/Vite build passed. |
| H-C5 | Export integration | `go test -race -tags sqlite_fts5 ./internal/workbench -run 'TestOfflineExport' -count=1`; `cd web && npx playwright test e2e/workbench.spec.ts --grep 'events jobs and offline export'` | Passed historically: offline export suite and 2 Chromium journeys, including offline rendering. |
| H-D1 | Localization/accessibility/fixtures | `cd web && npm run check && npx playwright test e2e/accessibility.spec.ts e2e/fixtures.spec.ts` | Final D1 result: TypeScript passed; 13 files / 102 unit tests; build passed; full web E2E 8 passed; focused D1 E2E 6 passed. |

### Commands observed on the D2 checkpoint worktree

| ID | Exact observed command | Fixture/environment | Observed result |
| --- | --- | --- | --- |
| F-PARITY | `go test -tags sqlite_fts5 ./internal/context ./internal/cli ./internal/mcp ./internal/workbench -run 'TestCanonicalContextProjection\|TestContextLensParity' -count=1` | Local Linux; CGO; SQLite FTS5 | Passed, 4 packages. |
| F-ROUTES | `go test -race -tags sqlite_fts5 ./internal/workbench ./internal/cli -run 'Test(Bootstrap\|Nonce\|OpenOutput\|BrowserLaunch\|RouteInventory\|RequestBounds\|PrincipalDerivation\|MutationAuth\|Explore\|GuidedTour\|SourcePreview\|Impact\|SSE\|JobRoutes\|Reconnect\|Shutdown\|Redaction\|OfflineExport)' -count=1` | Local Linux; real packages; race detector | Passed, 2 packages. |
| F-RECOVERY | `go test -race -tags sqlite_fts5 ./internal/events ./internal/jobs -run 'Test(CursorScope\|AdapterConformance\|ConnectedSubscriberSweep\|RetentionReset\|SlowSubscriber\|Job\|OutboxTransaction\|CommitBeforePublish\|PublisherWatermark\|Cancel\|Restart)' -count=1` | Local Linux; durable temporary jobs databases; race detector | Passed, 2 packages. |
| F-BROWSER | `cd web && npx playwright test e2e/workbench.spec.ts` | Compiled Go binary and committed `web/dist`; three copied real fixtures; loopback; pinned Chromium | Passed, 7 tests in 1.1 minutes. |
| F-TYPES | `cd web && npm run typecheck` | Local Node/TypeScript | Final rerun passed, no diagnostics. |

## A1–D2 requirement map

“Final” references only the D2 commands above. A historical command that was not
rerun is explicitly labeled as such.

| Task | Route or externally observable surface | Implementation path | Covering test/oracle | Evidence and result | Fixture/environment; exclusions/thresholds | Migration, rollback, restart; independent disposition |
| --- | --- | --- | --- | --- | --- | --- |
| A1 | CLI, MCP, LSP, and workbench share one project assembly/freshness boundary | `internal/application`, `internal/{cli,index,store,query,context,mcp,lsp}` | Generation guards, locking, assembly parity | Historical H-A1 and H-MA passed. Final D2 did not rerun the complete A1 race set; controller full/race gates pending. | Temporary stores/caches excluded; no volatile field projection involved. | Historical canonical handoff inherited; final independent requirements review pending. |
| A2 | `GET /api/v1/health`, `GET /api/v1/brief` | `internal/workbench/{service,api}.go`, bounded query/store/knowledge readers | Brief/API bounds, cancellation, identifier redaction | Historical H-A2 passed; final F-ROUTES and every F-BROWSER fixture Brief passed. | All three fixtures; request IDs/HTTP envelope excluded only where canonical comparison is not applicable. | No migration; final reviews pending. |
| A3 | `prowl-agent open`; bounded pre-listen startup and deferred refresh | `internal/cli/open.go`, `internal/application/project.go`, `internal/{boundedio,config,index,store,workspace,knowledge}` | Startup bound, special-file rejection, cleanup | Historical H-A1 passed; compiled launch/shutdown occurred in every F-BROWSER journey. Complete final startup race set pending controller. | 250 ms / 2,000 candidates are product thresholds; temporary workspace/state excluded. | Startup restart evidence historical; final reviews pending. |
| A4 | `POST /api/v1/auth/bootstrap`; one-time private handoff | `internal/workbench/bootstrap.go`, `internal/cli/open.go`, `web/src/transport/{auth,api}.ts` | Replay, expiry, hostile Host/Origin/fetch-site, redacted stdout/process metadata, no storage | Historical H-A4/H-A4W passed; final F-ROUTES and F-BROWSER passed, including real 60-second expiry. | Nonce/bearer values are never recorded. Browser storage/cookies/history must remain empty. | No migration; final security review pending. |
| A5 | Home/Brief screen and exact source fact | `web/src/features/brief`, `web/src/app`, embedded `web/dist` | Compiled vertical slice and accessibility | Historical H-A5/H-MA passed; final F-BROWSER passed all three fixture Briefs. | Real fixture facts; Playwright traces/results and temp roots excluded. | Bundle diff controller gate pending; final reviews pending. |
| B1 | `GET /api/v1/explore`, `GET /api/v1/tours/{tour_id}`, `GET /api/v1/source` | `internal/workbench/{explore,source}.go` | 5–12 deterministic rooted steps; every rendered anchor opens a bounded visible preview | Historical H-B1 passed; final F-ROUTES and F-BROWSER passed. D2 repaired unrooted steps regression-first. | Each fixed fixture returned 5 rooted steps. Source limit remains 400 lines / 128 KiB. | No migration; final reviews pending. |
| B2 | `POST /api/v1/context/search`, `POST /api/v1/context/get` | `internal/context/projection.go`, `internal/workbench/context_lens.go`, CLI/MCP serializers | Byte-identical canonical projection | Historical H-B2 and final F-PARITY passed; F-BROWSER exercised search/get on every fixture. | Only `trace_id`, request IDs, transport envelope fields, and explicitly tagged timestamps may be excluded. All order, IDs, citations, budgets, omissions, provenance, and freshness remain. | No migration; final requirements review pending. |
| B3 | `POST /api/v1/impact`; `GET /api/v1/knowledge[/{id}]`; `GET /api/v1/knowledge/proposals/{id}`; `GET /api/v1/timeline` | `internal/workbench/{impact,knowledge,timeline}.go` | Real read models and stable empty collections | Historical H-B3 passed; final F-ROUTES/F-BROWSER passed. D2 repaired nil Impact blast collections regression-first. | Limit ≤100 with opaque continuation. Fixture-backed empty states are evidence, not placeholders. | No migration; final reviews pending. |
| B4 | Proposal accept/reject routes | `internal/workbench/knowledge_mutation.go`, `internal/knowledge` decision transaction | Version, confirmation, idempotency, rollback | Historical H-B4 passed. Final F-ROUTES covered mutation auth/inventory; complete B4 final rerun pending controller. | Local principal is server-derived; no browser actor authority. | Historical rollback/idempotency evidence passed; final reviews pending. |
| B5 | Setup detect/plan/apply/verify routes | `internal/setup`, `internal/workbench/setup.go`, `internal/cli/setup.go` | No-write preview, approval, conflicts, verification, rollback | Historical H-B5 passed; final F-BROWSER exercised fixture-backed detect on every project and F-ROUTES covered mutation guards. | Credential presence only; setup resource version is the project-config SHA-256. | Historical backup/rollback and cross-platform compile evidence passed; final reviews pending. |
| B6 | Exact declared API method/path inventory | `internal/workbench/api.go` | Duplicate/unowned route rejection and request bounds | Historical H-B6 passed; final F-ROUTES passed. F-BROWSER asserts each named screen request against an explicit inventory route, not a route count. | Versioned `/api/v1/` boundary; no volatile exclusions. | No migration; final reviews pending. |
| B7a | Explore, Context Lens, Impact screens | `web/src/features/{explore,context,impact}`, `web/src/transport/contracts.ts` | Typed populated/empty/error views and source navigation | Historical H-B7A passed; final F-BROWSER exercised populated Context/Impact and rooted Explore for every fixture. | Real server DTOs only; browser computes no domain projection. | No migration; final reviews pending. |
| B7b | Knowledge, Timeline, Setup screens | `web/src/features/{knowledge,timeline,setup}` | Real list/empty states and guarded mutation UI | Historical H-B7B passed; final F-BROWSER exercised all three screens for every fixture. | Empty fixture collections are accepted only with a real 200 response/resource version. | Historical conflict/rollback UX passed; final reviews pending. |
| B7c | Shared shell, all B transport routes, bounded source screen, selected-context screen | `web/src/app/App.tsx`, `web/src/transport/api.ts`, `web/src/styles.css` | Authenticated same-origin route integration and focus | Historical H-B7C passed; final F-BROWSER explicitly observed Brief, Explore, tour, source, Context search/get, Impact, Knowledge, Timeline, and Setup routes on each fixture. | No production mock route. | No migration; final reviews pending. |
| C1 | Scoped SSE cursor semantics and commit-before-publish delivery | `internal/events/{cursor,outbox,broker}.go` | Cursor scope, retention reset, connected-subscriber sweep, slow consumer | Historical H-C1 and final F-RECOVERY passed. F-BROWSER observed replay/resume/reset. | Cursor tuple is `{stream_scope,scope_id,epoch,sequence}`; no global order. | Durable watermark/sweep repair passed; final security review pending. |
| C2 | Durable project jobs, startup refresh, cancel/restart | `internal/jobs`, `internal/events/project_jobs.go`, application/index/CLI handoff | Atomic job+outbox, cancellation, restart/reconcile, migration | Historical H-C2 passed; final F-RECOVERY and compiled cancellation in F-BROWSER passed. | Temporary jobs DB excluded; bounded/redacted job evidence only. | Historical v1 migration/restart passed; final reviews pending. |
| C3 | `GET /api/v1/events`; `GET /api/v1/jobs/{id}`; cancel and refresh routes | `internal/workbench/{events,jobs,api}.go` | Authenticated SSE, URL-credential rejection, reconnect/reset, shutdown, redaction | Historical H-C3 and final F-ROUTES passed; F-BROWSER exercised the compiled routes. | Keepalives are comments; payloads bounded and redacted. | Restart authority comes from C2; final security review pending. |
| C4 | Persistent Job Status, authenticated fetch stream, canonical refetch | `web/src/transport/events.ts`, `web/src/features/jobs/JobStatus.tsx`, App/API/styles | Replay/retry/reset/cancel UX | Historical H-C4 passed the production frontend path. Final F-BROWSER exercised compiled HTTP/SSE server replay, resume, reset, and terminal cancellation through the test client; it did not rerun `events.ts` or the Job Status UI. | Bearer remains module memory only; events are invalidations, not durable truth. | Final server reconnect/reset/cancel observed; final frontend recovery remains historical and pending final review. |
| C5 | `POST /api/v1/export`, `GET /api/v1/export/{id}`; offline HTML | `internal/workbench/export.go`, export runner, `web/e2e/workbench.spec.ts` | Offline render, strict CSP, no API/network/secret/private path | Historical H-C5 and final F-ROUTES/F-BROWSER passed. | Generated timestamp is the only export volatility. Nonce, bearer, `/api/v1/`, absolute temp roots, and network resources are absent. | Queued export restart is historical; final security review pending. |
| D1 | English/en-XA, accessibility, exact viewport fixture snapshots | `web/src/i18n`, localized feature/shell files, D1 E2E/snapshots | Axe, keyboard, reduced motion, 200% zoom, screenshots | Historical H-D1 passed on accepted commit `43e4f3f`. D1 E2E was intentionally not rerun during focused D2 work. | Exact 1280×800, 768×1024, 390×844; only `.brief-indexed` timestamp masked. | No migration; D1 review clean historically; final D2 reviews pending. |
| D2 | Final compiled requirements/security checkpoint | `web/e2e/workbench.spec.ts`, this matrix, D2 report; regression repairs in `internal/workbench` | F-PARITY, F-ROUTES, F-RECOVERY, F-BROWSER, production quality selector | Focused automated oracles passed. Participant, independent review, and controller full gates remain pending, so D2/Phase 3A is not accepted. | Three real fixtures; 5 rooted steps each; participant threshold below. | Final independent security and requirements dispositions pending. |

## Fixed fixture journeys observed by compiled Chromium

Each journey launches the real compiled binary, consumes the mode-0600 one-time
handoff, and traverses the production bundle. The browser explicitly observes
real 200 envelopes and resource versions for every screen route.

| Fixture | Deterministic journey | Observed automated result |
| --- | --- | --- |
| `go-auth-service` | Brief → Explore → 5-step onboarding tour → every source preview → Context query `access tokens` → selected context → Impact for the first tour source → Knowledge → Timeline → Setup | Passed. Fixture paths include `README.md`, `cmd/server/main.go`, `internal/auth/token.go`, and `internal/httpapi/server.go`. |
| `ts-checkout` | Same route sequence; Context query `checkout calculation` | Passed. Real fixture includes README/package metadata and cart/checkout/test sources. |
| `rice-config` | Same route sequence; Context query `Hyprland` | Passed. Real fixture includes README, Hyprland, Waybar, AwesomeWM, and shell automation sources. |

The route assertion is correspondence-based: it requires each explicit method and
route template (`brief`, `explore`, `tours/{tour_id}`, `source`, both Context
routes, `impact`, `knowledge`, `timeline`, and `setup/detect`) to be observed.
It does not infer coverage from the number of requests or routes.

## Canonical Context Lens volatility policy

The final parity command F-PARITY compares the shared canonical projection used
by CLI, MCP, and HTTP. The allowlist is closed:

- `trace_id`;
- request IDs;
- transport envelope fields;
- timestamps explicitly tagged volatile.

Selected IDs and order, summaries, token/byte budgets, omissions, provenance,
citations, confidence/freshness, and continuation guidance are not excluded.

## Newcomer comprehension protocol -- pending independent execution

### Instructions

Use only the compiled Workbench. Do not use the terminal, repository browser,
external documentation, another participant, or prior project knowledge. For
each fixture, start the timer when its authenticated Brief becomes visible.
Complete the fixed journey above, answer all five questions, and stop the timer
when the fifth answer is recorded. The per-fixture limit is 10 minutes.
Participants must be independent of D2 implementation and are anonymized as
P1–P3.

### Exact rubric

Each answer receives exactly 0 or 1 point.

1. **Purpose:** “What is this project for? Name one visible Workbench fact that
   supports your answer.” Score 1 only if both the fixture-specific purpose and
   a visible supporting fact are correct.
2. **Architecture boundary:** “Name one architecture or subsystem boundary and
   the source anchor that supports it.” Score 1 only if the boundary is
   fixture-specific and its cited Workbench anchor resolves.
3. **Evidence location:** “Where would a newcomer open the exact evidence for
   one behavior? Give the Workbench screen and project-relative path/line.”
   Score 1 only if the screen and resolving source location are both correct.
4. **Risky change area:** “Which displayed area would be risky to change, and
   what Workbench evidence makes it risky?” Score 1 only if the answer identifies
   a fixture source area and uses displayed Explore/Impact evidence rather than
   an unsupported guess.
5. **Next verification action:** “After changing that area, what is the next
   verification action indicated by the Workbench evidence?” Score 1 only if the
   action is specific and connected to a displayed test, entrypoint, impact, or
   refresh/source-verification path.

Acceptance requires at least 36/45 overall **and** at least 4/5 on every one of
the nine participant-fixture journeys. A journey over 10 minutes fails its
per-journey threshold regardless of answer score.

### Blank participant score sheet

All cells remain blank until the controller records real independent sessions.
“--” means unobserved, not zero.

| Participant | Fixture | Question | Score (0/1) | Time at answer | Anonymized answer/evidence | Disposition |
| --- | --- | --- | --- | --- | --- | --- |
| P1 | go-auth-service | 1 Purpose | -- | -- | -- | PENDING |
| P1 | go-auth-service | 2 Architecture boundary | -- | -- | -- | PENDING |
| P1 | go-auth-service | 3 Evidence location | -- | -- | -- | PENDING |
| P1 | go-auth-service | 4 Risky change area | -- | -- | -- | PENDING |
| P1 | go-auth-service | 5 Next verification action | -- | -- | -- | PENDING |
| P1 | ts-checkout | 1 Purpose | -- | -- | -- | PENDING |
| P1 | ts-checkout | 2 Architecture boundary | -- | -- | -- | PENDING |
| P1 | ts-checkout | 3 Evidence location | -- | -- | -- | PENDING |
| P1 | ts-checkout | 4 Risky change area | -- | -- | -- | PENDING |
| P1 | ts-checkout | 5 Next verification action | -- | -- | -- | PENDING |
| P1 | rice-config | 1 Purpose | -- | -- | -- | PENDING |
| P1 | rice-config | 2 Architecture boundary | -- | -- | -- | PENDING |
| P1 | rice-config | 3 Evidence location | -- | -- | -- | PENDING |
| P1 | rice-config | 4 Risky change area | -- | -- | -- | PENDING |
| P1 | rice-config | 5 Next verification action | -- | -- | -- | PENDING |
| P2 | go-auth-service | 1 Purpose | -- | -- | -- | PENDING |
| P2 | go-auth-service | 2 Architecture boundary | -- | -- | -- | PENDING |
| P2 | go-auth-service | 3 Evidence location | -- | -- | -- | PENDING |
| P2 | go-auth-service | 4 Risky change area | -- | -- | -- | PENDING |
| P2 | go-auth-service | 5 Next verification action | -- | -- | -- | PENDING |
| P2 | ts-checkout | 1 Purpose | -- | -- | -- | PENDING |
| P2 | ts-checkout | 2 Architecture boundary | -- | -- | -- | PENDING |
| P2 | ts-checkout | 3 Evidence location | -- | -- | -- | PENDING |
| P2 | ts-checkout | 4 Risky change area | -- | -- | -- | PENDING |
| P2 | ts-checkout | 5 Next verification action | -- | -- | -- | PENDING |
| P2 | rice-config | 1 Purpose | -- | -- | -- | PENDING |
| P2 | rice-config | 2 Architecture boundary | -- | -- | -- | PENDING |
| P2 | rice-config | 3 Evidence location | -- | -- | -- | PENDING |
| P2 | rice-config | 4 Risky change area | -- | -- | -- | PENDING |
| P2 | rice-config | 5 Next verification action | -- | -- | -- | PENDING |
| P3 | go-auth-service | 1 Purpose | -- | -- | -- | PENDING |
| P3 | go-auth-service | 2 Architecture boundary | -- | -- | -- | PENDING |
| P3 | go-auth-service | 3 Evidence location | -- | -- | -- | PENDING |
| P3 | go-auth-service | 4 Risky change area | -- | -- | -- | PENDING |
| P3 | go-auth-service | 5 Next verification action | -- | -- | -- | PENDING |
| P3 | ts-checkout | 1 Purpose | -- | -- | -- | PENDING |
| P3 | ts-checkout | 2 Architecture boundary | -- | -- | -- | PENDING |
| P3 | ts-checkout | 3 Evidence location | -- | -- | -- | PENDING |
| P3 | ts-checkout | 4 Risky change area | -- | -- | -- | PENDING |
| P3 | ts-checkout | 5 Next verification action | -- | -- | -- | PENDING |
| P3 | rice-config | 1 Purpose | -- | -- | -- | PENDING |
| P3 | rice-config | 2 Architecture boundary | -- | -- | -- | PENDING |
| P3 | rice-config | 3 Evidence location | -- | -- | -- | PENDING |
| P3 | rice-config | 4 Risky change area | -- | -- | -- | PENDING |
| P3 | rice-config | 5 Next verification action | -- | -- | -- | PENDING |

Participant totals, per-journey totals, elapsed times, and threshold disposition:
**PENDING -- no sessions have been run or scored.**

## Independent review dispositions

| Review | Reviewer | Checkpoint reviewed | Findings | Disposition |
| --- | --- | --- | --- | --- |
| D2 security review | -- | -- | -- | **PENDING** |
| D2 requirements/evidence review | -- | -- | -- | **PENDING** |

Historical task reviews remain historical evidence only; they do not substitute
for these final independent D2 dispositions.

## Remaining controller gates

The implementer intentionally did not claim or run these controller-owned final
gates during focused D2 work:

```sh
export PATH="$HOME/sdk/go/bin:$PATH" CGO_ENABLED=1
go test -tags sqlite_fts5 ./... -count=1
go vet -tags sqlite_fts5 ./...
go test -race -tags sqlite_fts5 ./internal/application ./internal/workbench ./internal/events ./internal/jobs ./internal/cli ./internal/mcp -count=1
cd web
npm ci
npm audit --audit-level=low
npm run check
cd ..
git diff --exit-code -- web/dist
git diff --check
```

After those commands, the controller must run the nine independent participant
journeys, record all 45 answers without prefill, and obtain independent security
and requirements dispositions on the committed D2 checkpoint. Until then the
correct status is `DONE_WITH_CONCERNS`, not Phase 3A accepted.
