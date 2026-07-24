# Phase 3A Completion Plan — Real Workbench

> **Execution contract:** Complete task IDs in order unless a dependency explicitly permits parallel work. A worker owns only the files and routes named by its assigned task. Shared seams (`internal/workbench/api.go`, `internal/workbench/handler.go`, `web/src/app/App.tsx`, `web/src/styles.css`, `web/src/transport/*`, `web/e2e/workbench.spec.ts`, and `web/dist/**`) have one checkpoint owner and are never edited concurrently. Every implementation task follows RED → focused GREEN → integration verification → review.

**Goal:** Turn the secured embedded shell at `95ba94f` into a real local knowledge workbench backed by the same deterministic project/query/context/knowledge services as CLI and MCP.

**Canonical contracts:**

- `docs/plans/2026-07-23-prowl-agent-product-evolution.md`
- `docs/plans/2026-07-23-hermes-inspired-agent-operations-port.md`
- `docs/plans/2026-07-23-product-evolution-handoff.md`

## Global invariants

- Bind IPv4 loopback only; enforce exact Host, Origin, and fetch-site policy.
- A fragment may contain only a short-lived single-use bootstrap nonce, never the API bearer. `POST /api/v1/auth/bootstrap` atomically consumes that nonce and returns a distinct process-memory-only bearer. Nonce replay and expiry fail closed. Automatic browser launch prints only the redacted origin; `--no-browser` reveals the sensitive nonce URL only through an interactive reveal or a mode-`0600` one-time file/FD, never ordinary stdout.
- Bearers, nonces, source bodies, prompts, and secrets never enter URLs used for API/SSE requests, browser storage, logs, events, exported HTML, or process metadata beyond the explicitly accepted browser-launch fragment exposure.
- APIs are versioned and bounded; the server owns canonical data and TypeScript owns no domain rules.
- A synchronous startup freshness probe is bounded to **250 ms and 2,000 candidate paths**. If freshness cannot be proven within either bound, listen with an authenticated `initializing` snapshot and run the full refresh as a durable cancellable job. A browser must be able to observe and cancel that job.
- Durable event cursors are scoped tuples `{stream_scope, scope_id, epoch, sequence}`; no cross-database total order is claimed.
- All long work is cancellable; acceptance uses the compiled binary and committed bundle; production has no placeholder data.

## Ownership and integration rules

1. `internal/application/**` and migrations of CLI/MCP construction are initially owned by A1. A3 receives the serialized bounded-startup handoff in `project.go`/`project_test.go`; C2 later receives the sole durable-job startup handoff. No other task edits those seams.
2. `internal/workbench/api.go` is the route-composition seam. A2 owns it through Milestone A; B6 owns the consolidated read/mutation routes; C3 owns event/job routes; C5 owns export. Those tasks are serialized.
3. `web/src/transport/auth.ts` and the async bootstrap call in `web/src/main.tsx` are owned only by A4 for nonce-to-bearer bootstrap. `web/src/transport/api.ts` is owned by A4, then extended only by C4 after B tasks expose stable DTOs.
4. `web/src/app/App.tsx` and `web/src/styles.css` are shared-shell seams. A4, B7, C4, and D1 edit them sequentially. Feature tasks otherwise stay under their named feature directory.
5. `web/e2e/workbench.spec.ts`, fixture orchestration, and `web/dist/**` are integration outputs owned only by A5, C5, D1, and D2 at their serialized checkpoints. Feature workers do not rebuild or commit `web/dist`.
6. `internal/events/**` is created and owned by C1. Phase 3B/3C must extend its stable interfaces rather than introduce another event package. `internal/jobs/**` is created and owned by C2.
7. No task may widen ownership silently. A prerequisite expansion is recorded in this plan before dispatch.

## Milestone A — One real connected fact and secure bootstrap

### Task A1: Deterministic project application assembly

**Files:**

- Modify `go.mod` and `go.sum` for the A1 cross-process lock dependency
- Create/finish `internal/application/project.go`
- Create/finish `internal/application/project_test.go`
- Modify `internal/cli/{query.go,context.go,status.go,doctor.go,serve.go,lsp.go,init.go,restart.go,freshness.go}`
- Create/modify `internal/cli/{cli_test.go,context_test.go,project_assembly_test.go,reindex_test.go,restart_test.go,freshness_test.go,lsp_linux_test.go}`
- Modify `internal/index/{index.go,index_test.go,embed.go,walk.go,watch.go,watch_test.go}`
- Modify `internal/store/{store.go,store_test.go,reset.go,vectors.go,vectors_test.go,files.go,graph.go,knowledge.go,queries.go,stats.go,context.go,context_runs.go,doctor_reads.go,entities.go,lsp_reads.go}` only for the outer generation transaction, metadata publication, read-generation guard, and nested-write reuse required by A1
- Modify `internal/query/{query.go,overview.go,query_test.go}` and `internal/context/{service.go,service_test.go}` only for fail-closed generation reads
- Modify `internal/mcp/{server.go,server_options.go,resources.go,server_test.go,resources_test.go}` only for propagated pre-call freshness errors and indexed-resource gating
- Create/modify `internal/lsp/{server.go,server_test.go,handlers.go,input_linux.go,input_other.go,input_linux_test.go}` only for application-injected generation guards, fail-closed refresh-error propagation, refresh-result ordering, and cancellable inherited stdin

**Contract:**

- `application.OpenProject(ctx, start, options)` resolves the workspace, opens the derived store, fails on malformed configuration, synchronously establishes freshness for CLI/MCP/LSP callers, and assembles query, knowledge, context, capability, and serialized refresh services. A1 starts no goroutines; A3 owns the bounded workbench probe/deferred-open seam through an explicit owner handoff.
- `Project.Refresh(ctx)` uses a context-cancellable local gate plus exclusive `.prowl/index-refresh.lock` across processes. Application-assembled query/context/LSP readers hold a shared lock for each complete logical operation, so a multi-statement operation is pinned to one committed generation and waits rather than straddling a commit. Each refresh attempt writes structural metadata, graph rows, and vector rows inside one SQLite generation transaction. It binds the pre-index signature to the exact walked path set, accepts publication only when the identical post-index signature matches, every indexed row still matches the final normalized bytes, and every pre-snapshot path that remains indexable has a corresponding row. This bidirectional check rejects transient edits and disappearance/restoration ABA omissions while applying the same generated-marker stripping used during indexing. Read/stat failures are typed source-change failures; refresh retries one changed generation before setting `index_state=incomplete` and failing closed. Structural refresh clears `vectors_complete`; vector batches remain unpublished on any failure; successful embedding sets completeness only after exact `embed_model` metadata matches, including repair of missing/unknown model metadata.
- The returned project has explicit idempotent `Close()` and owns the store. Transport watchers may call `Project.Refresh(ctx)` but may not rebuild index/config/AI assembly. Every watcher owner propagates registration/runtime failures into dirty or fail-closed state, cancels and joins watcher callbacks before closing the project, and never discards LSP/MCP refresh errors. Concurrent LSP watcher/save refreshes reserve monotonic IDs before starting; only the latest-started result may update sticky health state or republish diagnostics. LSP wraps inherited stdin with an OS-specific cancellable input; Linux uses `poll(2)` plus a private wake pipe rather than relying on cross-goroutine descriptor close. `Run` invokes `CancelRead` on context cancellation, joins the cancellation helper, and only then permits watcher/project teardown.
- Migrate `RunInit`, `openQuerier`, `openContextService`, status, context traces, doctor, `newServeCmd`, and `newLSPCmd`; no structural read path may discard project/rules configuration errors, directly call `index.Index*`, independently open the derived store, or independently assemble equivalent services.
- CLI, MCP, and workbench adapters receive the same project services. Optional AI unavailability preserves deterministic behavior; malformed AI/project configuration does not silently become defaults.

**RED:** add fixture tests for parent resolution, malformed config through query/context/MCP/LSP adapters, interrupted-generation repair, nested/rollback/commit-failure generation handling, local/process-lock cancellation, logical-read generation pinning across a second SQLite connection, pre/post-signature and disappearance/restoration ABA publication rejection, model-change vector rebuild, joined watcher shutdown and watcher-failure recovery, context-cancellable freshness serialization, close during active refresh, init/restart migration, no-model operation, legacy/all MCP pre-call gating, LSP refresh fail-closed/result-ordering recovery, real inherited-stdin cancellation through the CLI composition, indexed-resource gating, and canonical context/query output equality.

**Focused command:**

```sh
go test -race -tags sqlite_fts5 ./internal/application ./internal/store ./internal/index ./internal/query ./internal/context ./internal/cli ./internal/mcp ./internal/lsp -run 'Test(OpenProject|ProjectRefresh|ProjectClose|Signature|Vector|PublishedGeneration|LogicalQueryPinsGeneration|GenerationRejectsNested|ValidateSnapshotAcceptsGeneratedAgentsMarker|CLI_MCP_LSPProjectParity|Open.*MalformedConfig|StatusRefreshesThroughProject|ContextTracesReturnsMalformedConfigError|DoctorRejectsMalformedRules|LSPRejectsMalformedRules|Reindexer|RunInit|RestartRefreshesThroughProject|Freshness|WatchCancellationJoinsActiveCallback|WatchRejectsMissingRoot|TrackedRejects|LegacyAndAllSurfacesApplyBeforeCall|ResourcesListReadAndRejectTraversal|ResetDerivedInvalidatesPublishedGeneration|InterruptedIndexIsMarkedIncompleteAndRecovers|Definition|LSPRefresh|LSPRun)' -count=1
```

**Expected RED/GREEN:** before implementation, at least one named test fails because construction or error behavior differs; after implementation, all selected tests pass and no adapter-local `config.Load(...), _` construction remains.

### Task A2: Versioned API envelope and real Brief service

**Depends on:** A1

**Files/routes:**

- Create `internal/workbench/service.go`
- Create `internal/workbench/service_test.go`
- Modify `internal/workbench/api.go`
- Modify `internal/workbench/api_test.go`
- Modify `internal/workbench/handler_test.go` only to assert fail-closed behavior before A3 injects the real service
- Modify `internal/query/overview.go`, `internal/query/query.go`, and `internal/query/query_test.go` only to add a context-aware, hard row-bounded Overview projection and deterministic cancellation seam while preserving the existing `Overview()` API
- Create `internal/store/overview_reads.go` for SQL-level context/row-limited read variants used only by bounded Overview
- Modify `internal/knowledge/repository.go` and `internal/knowledge/repository_test.go` only to add a context-aware `List` variant with hard document and total-entry caps plus one pinned rooted handle, while preserving `List()` and `ListContext(ctx, maxDocuments)`
- Own `GET /api/v1/health`
- Own `GET /api/v1/brief`

**Contract:** typed bounded Brief from real overview, knowledge health, workspace identity, freshness, and capabilities. Bounds apply to database/filesystem input work, aggregate SQL scans, and the complete HTTP envelope, not only post-hoc DTO truncation; request cancellation reaches SQL and knowledge traversal. Knowledge listing caps every visited filesystem entry and pins one rooted directory handle across enumeration and reads so a root rename/symlink swap cannot change containment. The complete Brief projection holds the project read guard so one response cannot mix generations. Every derived-store string is validated before output and malformed absolute/traversing/path-bearing metadata fails closed without reflection. General endpoints use `data`, `meta.request_id`, and `meta.resource_version`; `resource_version` is the canonical resource data version (the published project signature for health/Brief), not the API schema label. Pre-resource/security errors use a stable `unavailable` resource version; failures after a version is known preserve it. All API errors, including Host/Origin/fetch-site/authentication/router failures, use stable bounded JSON `error.code` and `error.message` envelopes. A missing real service fails closed. Context packet routes later return canonical `prowl.context/v1` data, not this envelope.

**Focused command:**

```sh
go test -race -tags sqlite_fts5 ./internal/workbench ./internal/query ./internal/store ./internal/knowledge -run 'Test(Brief|API|Projection|MalformedResourceVersion|IdentifierValidation|SuccessWriter|ErrorWriter|Overview|RepositoryList|RepositoryRootSwap)' -count=1
```

**Expected:** unauthorized/hostile requests fail; success, empty, malformed-store, cancellation, path-redaction, ordering, and response-bound tests pass.

### Task A3: Bounded startup and `prowl-agent open`

**Depends on:** A1–A2

**Files:**

- Modify `internal/cli/open.go`
- Modify `internal/cli/open_test.go`
- Modify `internal/application/project.go` only through A1 owner handoff
- Modify `internal/application/project_test.go` only for bounded workbench startup coverage
- Create `internal/boundedio/{boundedio.go,open_unix.go,open_other.go}` for rooted, descriptor-validated, nonblocking deadline-sensitive reads
- Modify `internal/config/{config.go,config_test.go}` only to add bounded context-aware config loading while preserving `Load`
- Modify `internal/index/walk.go` and `internal/index/index_test.go` only to add a context-cancellable candidate-path cap that reuses the canonical ignore/walk rules
- Modify `internal/store/{store.go,store_test.go}` only to add context-aware bounded store assembly while preserving `Open`
- Modify `internal/workspace/{workspace.go,workspace_test.go}` only to bound workbench workspace discovery while preserving synchronous `Resolve`
- Modify `internal/knowledge/{repository.go,repository_test.go}` to ensure the context-aware A2 projection path cannot block while opening special files

**Contract:** resolve configuration and complete one freshness probe under a single 250 ms deadline and a hard 2,000-candidate-path cap before listen. Workspace discovery returns on deadline even when a metadata syscall stalls. Deadline-sensitive configuration, ignore, source, and context-aware knowledge inputs are opened nonblocking through pinned roots, descriptor-validated as regular files, and read with context checks; special files cannot strand startup or workbench requests. Workbench store assembly uses context-aware schema/migration operations and clamps SQLite busy wait to the remaining startup deadline. The bounded source signature remains canonically identical for unchanged ordinary files and in-root file symlinks while content reads stay confined to the accepted root. The path cap aborts canonical traversal before stat, read, or hashing of candidate 2,001; it does not duplicate ignore rules or reopen the accepted root by mutable pathname. If either bound is exceeded, listen with `initializing` state and enqueue the C2 refresh job once C2 exists; until then return a typed `startup_refresh_required` error without listening or continuing unbounded work. Ordinary config/store/index and CLI/MCP/LSP `OpenProject` behavior remains synchronously fresh. Close project/listener exactly once and preserve launcher reaping and graceful shutdown.

**Focused command:**

```sh
go test -race -tags sqlite_fts5 ./internal/cli ./internal/application ./internal/config ./internal/index ./internal/store ./internal/workspace ./internal/knowledge -run 'Test(OpenCommand|StartupFreshness|OpenProjectClose|LoadContext|OpenContext|ResolveContext|RepositoryListContextBoundedRejectsFIFO|.*Candidate)' -count=1
```

**Expected:** malformed setup never listens; a synthetic slow/large fixture returns within the configured bound; all cleanup assertions pass.

### Task A4: Nonce-to-bearer bootstrap

**Depends on:** A2–A3

**Files/routes:**

- Create `internal/workbench/bootstrap.go`
- Create `internal/workbench/bootstrap_test.go`
- Modify `internal/workbench/api.go`
- Modify `internal/workbench/api_test.go`
- Modify `internal/workbench/handler_test.go` to remove static-token construction; A4 permits no legacy bearer fallback
- Modify `internal/cli/open.go`
- Modify `internal/cli/open_test.go`
- Modify `web/src/transport/auth.ts`
- Modify `web/src/transport/auth.test.ts`
- Modify `web/src/main.tsx` only to await the fragment-to-bearer bootstrap before rendering authenticated UI
- Create `web/src/transport/api.ts`
- Create `web/src/transport/api.test.ts`
- Own `POST /api/v1/auth/bootstrap`; all other `/api/v1/*` require the minted bearer

**Contract:** generate separate 256-bit nonce and bearer values. The nonce has a 60-second TTL, is atomically single-use, and is accepted only from the exact same-origin bootstrap request. Its successful exchange invalidates it before returning a bearer. The frontend removes the fragment before any asynchronous exchange step, keeps the resulting bearer only in module memory, and never attaches authorization outside a normalized same-origin `/api/v1/` path; authorization-bearing fetches reject redirects. Automatic launch does not print the fragment. `--no-browser` defaults to a permission-`0600` one-time bootstrap file and supports explicit interactive reveal only when stderr/stdin are TTYs; CI/non-TTY stdout is always redacted. Document that the browser launcher necessarily receives the nonce URL as one process argument and that the short TTL/single use bounds that exposure.

**Focused commands:**

```sh
go test -race -tags sqlite_fts5 ./internal/workbench ./internal/cli -run 'Test(Bootstrap|Nonce|OpenOutput|BrowserLaunch)' -count=1
cd web && npm test -- --run src/transport/auth.test.ts src/transport/api.test.ts
```

**Expected:** replay, expiry, wrong origin, and nonce-as-bearer fail; automatic/non-TTY stdout contains neither nonce nor bearer; mode-`0600` handoff and distinct minted bearer pass; system prompt/storage are irrelevant and untouched.

### Task A5: Frontend Home/Brief and compiled-binary vertical acceptance

**Depends on:** A1–A4

**Files:**

- Create `web/src/features/brief/BriefPage.tsx`
- Create `web/src/features/brief/BriefPage.test.tsx`
- Modify `web/src/app/App.tsx`
- Modify `web/src/app/App.test.tsx`
- Modify `web/src/styles.css`
- Modify `web/e2e/workbench.spec.ts`
- Modify `web/playwright.config.ts` only if required
- Create fixture `testdata/workbench/go-auth-service/**`
- Regenerate `web/dist/**` only after all source tests pass

**Contract:** render real Brief loading/initializing/empty/error/populated states. Compiled-binary Playwright initializes the fixture, obtains the bootstrap URL from the protected one-time handoff, exchanges nonce, and asserts one canonical fact plus unauthorized/Host/Origin/replay/expiry/history/storage/refresh/accessibility/shutdown behavior.

**Commands:**

```sh
cd web && npm run check
cd .. && go test -tags sqlite_fts5 ./... -count=1
git diff --exit-code -- web/dist
```

**Expected:** typecheck, unit, build, and compiled-binary Chromium pass; tagged Go suite passes; a second clean frontend build leaves `web/dist` unchanged.

**Milestone A executable exit:** `go test -tags sqlite_fts5 ./internal/application ./internal/cli ./internal/mcp ./internal/workbench -run 'TestCLI_MCP_WorkbenchProjectParity|TestBootstrap' -count=1` and `cd web && npx playwright test e2e/workbench.spec.ts --grep 'brief vertical slice'` both exit 0. The fixture assertion identifies the same source anchor through CLI JSON, MCP structured content, API, and UI after canonical normalization.

## Milestone B — Read-only exploration, then explicit mutations

### Task B1: Explore read model

**Depends on:** Milestone A

**Files/routes:**

- Create `internal/workbench/explore.go`
- Create `internal/workbench/explore_test.go`
- Create `internal/workbench/source.go`
- Create `internal/workbench/source_test.go`
- Own `GET /api/v1/explore`
- Own `GET /api/v1/tours/{tour_id}`
- Own `GET /api/v1/source?path=&line_start=&line_end=`

**DTO/mutation ownership:** Go DTOs live in `explore.go`; no writes or migrations. Source reads reuse rooted/symlink-safe knowledge/MCP protections, cap ranges at 400 lines/128 KiB, and return exact relative anchors.

**RED/GREEN command:**

```sh
go test -race -tags sqlite_fts5 ./internal/workbench -run 'Test(Explore|GuidedTour|SourcePreview)' -count=1
```

**Expected:** RED before routes exist; GREEN with deterministic ordering, traversal/symlink rejection, bounds, and three 5–12-step tour fixtures.

### Task B2: Canonical Context Lens read model

**Depends on:** B1

**Files/routes:**

- Create `internal/workbench/context_lens.go`
- Create `internal/workbench/context_lens_test.go`
- Create `internal/context/projection.go`
- Create `internal/context/projection_test.go`
- Modify `internal/cli/context_test.go`
- Modify `internal/mcp/core_tools_test.go`
- Own `POST /api/v1/context/search`
- Own `POST /api/v1/context/get`

**DTO ownership:** `internal/context.Packet` remains canonical. `CanonicalProjection` removes only `trace_id`, request IDs, transport envelope fields, and timestamps explicitly tagged volatile; it preserves selected IDs/order, summaries, budgets, omissions, provenance, citations, and freshness. API request bodies are 64 KiB maximum and IDs/questions are bounded.

**RED/GREEN command:**

```sh
go test -tags sqlite_fts5 ./internal/context ./internal/cli ./internal/mcp ./internal/workbench -run 'TestCanonicalContextProjection|TestContextLensParity' -count=1
```

**Expected:** byte-identical canonical JSON for CLI, MCP, and HTTP fixture projections; any non-allowlisted difference fails with a field diff.

### Task B3: Impact, Knowledge, and Timeline read models

**Depends on:** B2

**Files/routes:**

- Create `internal/workbench/impact.go`, `internal/workbench/impact_test.go`
- Create `internal/workbench/knowledge.go`, `internal/workbench/knowledge_test.go`
- Create `internal/workbench/timeline.go`, `internal/workbench/timeline_test.go`
- Own `POST /api/v1/impact`
- Own `GET /api/v1/knowledge`, `GET /api/v1/knowledge/{id}`, `GET /api/v1/knowledge/proposals/{id}`
- Own `GET /api/v1/timeline`

**DTO/migration ownership:** DTOs stay in their named files; no migration. Bounded pagination is `limit<=100` with opaque continuation. Timeline initially merges only deterministic Git, knowledge-log, and privacy-safe context-trace metadata and labels provenance; it does not synthesize narrative.

**RED/GREEN command:**

```sh
go test -race -tags sqlite_fts5 ./internal/workbench -run 'Test(Impact|KnowledgeRead|Timeline)' -count=1
```

**Expected:** exact evidence anchors, stable ordering/pagination, proposal diffs, redaction, and empty states pass.

### Task B4: Knowledge proposal mutations

**Depends on:** B3

**Files/routes:**

- Create `internal/workbench/knowledge_mutation.go`
- Create `internal/workbench/knowledge_mutation_test.go`
- Modify `internal/knowledge/proposal.go` and `internal/knowledge/proposal_atomic_test.go` only if an adapter-neutral expected-version method is missing
- Own `POST /api/v1/knowledge/proposals/{id}/accept`
- Own `POST /api/v1/knowledge/proposals/{id}/reject`

**Mutation contract:** require explicit user confirmation, proposal `expected_version`, idempotency key, rooted rollback plan, immutable audit result, and conflict response. Keep accepted Markdown canonical. An approval decision is server-derived from the authenticated local principal; clients cannot submit authoritative actor IDs.

**RED/GREEN command:**

```sh
go test -race -tags sqlite_fts5 ./internal/knowledge ./internal/workbench -run 'Test(ProposalAPI|ExpectedVersion|Rollback|Idempot)' -count=1
```

**Expected:** stale version, duplicate key, rollback failure, and path escape are observable and safe; accept/reject audit assertions pass.

### Task B5: Setup read model and mutation

**Depends on:** B4

**Files/routes:**

- Create `internal/setup/service.go`
- Create `internal/setup/service_test.go`
- Modify `internal/cli/setup.go` and `internal/cli/setup_test.go` to call that service
- Create `internal/workbench/setup.go`
- Create `internal/workbench/setup_test.go`
- Own `GET /api/v1/setup/detect`
- Own `POST /api/v1/setup/plan`
- Own `POST /api/v1/setup/apply`
- Own `POST /api/v1/setup/verify`

**DTO/mutation ownership:** setup DTOs live in `internal/setup`. `apply` requires plan hash, expected project-config version, explicit approval, idempotency key, pre-write backup/rollback manifest, and post-apply verification. Responses report credential presence only. No setup operation writes before a reviewed plan.

**RED/GREEN command:**

```sh
go test -race -tags sqlite_fts5 ./internal/setup ./internal/cli ./internal/workbench -run 'TestSetup(Detect|Plan|Apply|Verify|Conflict|Rollback|Audit)' -count=1
```

**Expected:** preview performs zero writes; conflicts and denied approval perform zero writes; apply is idempotent and verified or rolled back with an explicit error.

### Task B6: Read/mutation route integration owner

**Depends on:** B1–B5

**Files:**

- Modify `internal/workbench/api.go`
- Modify `internal/workbench/api_test.go`
- Modify `internal/workbench/service.go`
- Modify `internal/workbench/service_test.go`

**Route ownership:** register exactly the B1–B5 routes above, centralize body/query/pagination limits and principal derivation, and reject duplicate/unowned routes. This task does not add domain behavior.

**RED/GREEN command:**

```sh
go test -race -tags sqlite_fts5 ./internal/workbench -run 'Test(RouteInventory|RequestBounds|PrincipalDerivation|MutationAuth)' -count=1
```

**Expected:** an exact route-method golden passes; all mutation routes enforce versions/approval/idempotency and all read routes enforce bounds.

### Task B7a: Read-only analysis views

**Depends on:** B6

**Files:** create `web/src/features/explore/{ExplorePage.tsx,ExplorePage.test.tsx}`, `web/src/features/context/{ContextLensPage.tsx,ContextLensPage.test.tsx}`, `web/src/features/impact/{ImpactPage.tsx,ImpactPage.test.tsx}`, and `web/src/transport/contracts.ts`.

**Contract/gate:** render loading/empty/error/populated states from typed B1–B3 contracts with keyboard-complete source navigation and no duplicated domain logic. Run `cd web && npm test -- --run src/features/explore src/features/context src/features/impact` → PASS.

### Task B7b: Knowledge, timeline, and setup views

**Depends on:** B7a

**Files:** create `web/src/features/knowledge/{KnowledgePage.tsx,KnowledgePage.test.tsx}`, `web/src/features/timeline/{TimelinePage.tsx,TimelinePage.test.tsx}`, and `web/src/features/setup/{SetupPage.tsx,SetupPage.test.tsx}`.

**Contract/gate:** keep mutations behind diff/preview/confirmation, send expected versions and idempotency keys through injected typed clients, restore focus after conflicts, and contain no replicated transition/setup/knowledge rules. Run `cd web && npm test -- --run src/features/knowledge src/features/timeline src/features/setup` → PASS for success/conflict/rollback/keyboard cases.

### Task B7c: Serialized transport and shell integration

**Depends on:** B7b

**Files:** modify `web/src/transport/api.ts` and tests, `web/src/app/App.tsx`, `web/src/app/App.test.tsx`, and `web/src/styles.css`. This is the sole B7 owner of shared transport/shell files.

**Contract/gate:** wire every B route through the production authenticated client, preserve route-level loading/error recovery and focus, and include no mock-only production route. Run `cd web && npm test -- --run src/features src/transport/api.test.ts src/app/App.test.tsx && npm run build` → PASS.

**Milestone B executable exit:** the Context Lens parity command in B2 exits 0; the setup/knowledge mutation commands in B4–B5 exit 0; `cd web && npm test -- --run src/features` exits 0 with no mock-only production route.

## Milestone C — Scoped outbox events, cancellable jobs, live data, and export

### Task C1: Scoped cursor and broker contract

**Depends on:** Milestone B

**Files/migrations:**

- Create `internal/events/cursor.go`, `internal/events/cursor_test.go`
- Create `internal/events/outbox.go`, `internal/events/outbox_test.go`
- Create `internal/events/broker.go`, `internal/events/broker_test.go`

**Contract:** cursor is `{stream_scope:"project-job", scope_id:<workspace-id>, epoch:<jobs-db-generation>, sequence:<outbox-row-sequence>}`. C1 defines the authority-neutral cursor, outbox adapter, publisher, and broker contracts with an in-memory conformance store; C2 supplies the durable `project-job` adapter. Future operations and board stores own separate adapters, outboxes, and cursors. Aggregators merge streams without a total order. The contract requires a durable publisher watermark plus a bounded sweep (triggered after commits and at most every 2 seconds while subscribers exist; no idle polling) to repair commit-before-publish failures for already-connected clients. Retention expiry emits reset with snapshot URI and current epoch; broker queues are bounded.

**RED/GREEN command:**

```sh
go test -race -tags sqlite_fts5 ./internal/events -run 'Test(CursorScope|AdapterConformance|ConnectedSubscriberSweep|RetentionReset|SlowSubscriber)' -count=1
```

**Expected:** the conformance adapter proves forced publish failure after commit reaches an already-connected subscriber without reconnect; rollback emits nothing; cross-scope cursor use resets; queue overflow resets rather than grows. C2 must repeat transaction and crash tests against the durable jobs database.

### Task C2: Durable cancellable project jobs

**Depends on:** C1

**Files/migrations:**

- Create `internal/jobs/store.go`, `internal/jobs/store_test.go`, `internal/jobs/model.go`, `internal/jobs/service.go`
- Create `internal/jobs/service_test.go`, `internal/jobs/restart_test.go`, `internal/jobs/outbox_test.go`
- Create `internal/jobs/migrations/001_project_jobs_outbox.sql`
- Create `internal/jobs/testdata/{v0.sql,v1.sql}`
- Create `internal/events/project_jobs.go`, `internal/events/project_jobs_test.go` as the `project-job` authority adapter
- Modify `internal/application/project.go` and `internal/application/project_test.go` through serialized owner handoff
- Modify `internal/index/index.go` and `internal/index/index_test.go` only to expose bounded progress/cancellation hooks
- Modify `internal/cli/open.go` and `internal/cli/open_test.go` through the serialized A3 owner handoff to replace `startup_refresh_required` with durable job enqueue/resume wiring

**Contract:** `internal/jobs.Store` owns `$XDG_DATA_HOME/prowl-agent/projects/<workspace-id>/jobs.db`; it never writes durable job state into the rebuildable `.prowl/index.db`. Schema `prowl.project-jobs/v1` stores jobs, outbox rows, authority epoch, retention floor, and publisher watermark. Each job mutation and its outbox append commit in one jobs-DB transaction. Indexing, research, and setup apply use job IDs, expected versions, phase/progress, bounded evidence/log tail, cancellation, and terminal outcome. C2 is the sole serialized owner of the A3 deferred-startup handoff: startup work beyond A3's probe enqueues or resumes exactly one index job through `internal/cli/open.go`, injects that job service into the workbench runtime, and no longer returns `startup_refresh_required`. Cancellation is durable and checked between bounded index batches.

**RED/GREEN command:**

```sh
go test -race -tags sqlite_fts5 ./internal/jobs ./internal/events ./internal/application ./internal/index ./internal/cli -run 'Test(Job|JobsDBPath|OutboxTransaction|CommitBeforePublish|PublisherWatermark|StartupRefreshJob|OpenStartupJobWiring|Cancel|Restart)' -count=1
```

**Expected:** synthetic long index is visible and cancellable after listen, restart reconciles nonterminal jobs, and no pre-listen path exceeds the startup bound.

### Task C3: Authenticated SSE and job routes

**Depends on:** C1–C2

**Files/routes:**

- Create `internal/workbench/events.go`, `internal/workbench/events_test.go`
- Create `internal/workbench/jobs.go`, `internal/workbench/jobs_test.go`
- Modify `internal/workbench/api.go`, `internal/workbench/api_test.go`
- Own `GET /api/v1/events?stream_scope=project-job&scope_id=&epoch=&sequence=`
- Own `GET /api/v1/jobs/{id}`
- Own `POST /api/v1/jobs/{id}/cancel`
- Own `POST /api/v1/index/refresh`

**Contract:** bearer-authenticated fetch-streamed SSE only; cursor fields are explicit and scope-bound. Keepalives are comments. Disconnect cancels subscriber context. Payloads are bounded redacted invalidations. Cancel requires job expected version and idempotency key.

**RED/GREEN command:**

```sh
go test -race -tags sqlite_fts5 ./internal/workbench ./internal/events ./internal/jobs -run 'Test(SSE|JobRoutes|Reconnect|Shutdown|Redaction)' -count=1
```

**Expected:** unauthorized and URL-credential attempts fail; reconnect/replay/reset, shutdown, cancel conflict, and redaction pass.

### Task C4: Frontend live-state client

**Depends on:** C3

**Files:**

- Create `web/src/transport/events.ts`, `web/src/transport/events.test.ts`
- Create `web/src/features/jobs/JobStatus.tsx`, `web/src/features/jobs/JobStatus.test.tsx`
- Modify `web/src/transport/api.ts`
- Modify `web/src/app/App.tsx`, `web/src/app/App.test.tsx`, `web/src/styles.css`

**Contract:** use authenticated `fetch`, not `EventSource`; events invalidate and debounce canonical refetch. Handle offline/reconnect, scope/epoch change, reset, slow consumer, cancellation, and stale optimistic versions. Bearer remains in memory.

**RED/GREEN command:**

```sh
cd web && npm test -- --run src/transport/events.test.ts src/features/jobs/JobStatus.test.tsx src/app/App.test.tsx && npm run build
```

**Expected:** no client event reducer becomes durable truth; reset refetches snapshot; cancellation and stale version UX pass.

### Task C5: Single-file offline export and integration ownership

**Depends on:** C4

**Files/routes:**

- Create `internal/workbench/export.go`, `internal/workbench/export_test.go`
- Modify `internal/workbench/api.go`, `internal/workbench/api_test.go`
- Own `POST /api/v1/export`
- Modify `web/e2e/workbench.spec.ts`
- Regenerate `web/dist/**`

**Contract:** read-only bounded HTML with inline assets/data, provenance, generated-at/source versions, no nonce/bearer/API calls, strict offline CSP, and deterministic content apart from an allowlisted generated timestamp. Export is a bounded job when generation exceeds request limits.

**RED/GREEN commands:**

```sh
go test -race -tags sqlite_fts5 ./internal/workbench -run 'TestOfflineExport' -count=1
cd web && npm run check
```

**Expected:** offline Chromium renders with networking disabled; secret scanner finds no nonce/bearer/private absolute path; compiled-binary reconnect/cancel/export journeys pass.

**Milestone C executable exit:** C1 commit-before-publish command, C2 restart/cancel command, and `cd web && npx playwright test e2e/workbench.spec.ts --grep 'events jobs and offline export'` each exit 0.

## Milestone D — Deterministic Phase 3A acceptance

### Task D1: Localization, accessibility, visual fixtures

**Depends on:** Milestone C

**Files:**

- Create `web/src/i18n/{en.ts,en-XA.ts,index.ts,index.test.ts}`
- Modify every `web/src/features/**/*.tsx` only for message IDs
- Modify `web/src/app/App.tsx`, `web/src/app/App.test.tsx`, `web/src/styles.css`
- Create fixtures `testdata/workbench/ts-checkout/**` and `testdata/workbench/rice-config/**`
- Create `web/e2e/accessibility.spec.ts`, `web/e2e/fixtures.spec.ts`
- Create/update `web/e2e/snapshots/**`
- Regenerate `web/dist/**`

**Oracle:** no hardcoded user-facing prose outside locale catalogs; English and `en-XA` pass. At 1280×800, 768×1024, and 390×844, axe has zero violations and zero serious incomplete findings; keyboard-only journeys complete; reduced motion and 200% zoom do not hide actions; visual snapshots match exactly under pinned Chromium.

**Command:**

```sh
cd web && npm run check && npx playwright test e2e/accessibility.spec.ts e2e/fixtures.spec.ts
```

**Expected:** all three fixtures and all viewport/accessibility assertions pass.

### Task D2: Final compiled-binary requirements and security audit

**Depends on:** D1

**Files:**

- Modify `web/e2e/workbench.spec.ts`
- Create `docs/verification/phase-3a-requirements.md`
- Regenerate `web/dist/**` only if source changed during blocker fixes

**Executable oracles:**

1. Canonical context equality: B2's canonical projection command exits 0; excluded volatile fields are only `trace_id`, request IDs, transport envelope fields, and tagged timestamps.
2. Newcomer comprehension: three independent participants each complete all three fixed fixture journeys and answer five rubric questions per fixture—purpose, architecture boundary, evidence location, risky change area, and next verification action—within 10 minutes per fixture using only the workbench. Record 45 anonymized binary-scored answers and the exact rubric in the verification file. Threshold is at least 36/45 overall and at least 4/5 for every participant-fixture journey.
3. Guided tours: each fixture exposes 5–12 rooted steps; every step's anchor resolves and is visible; Playwright asserts completion and evidence navigation.
4. Security/recovery: nonce replay/expiry, process/stdout redaction, hostile Host/Origin/fetch-site, no storage, scoped cursor reconnect/reset, commit-before-publish repair, cancellation, and exported-secret absence all pass.
5. Real-path quality: every screen's request appears in the route inventory and returns fixture-derived data; a source scan rejects `placeholder|TODO metric|Math.random` in production frontend files.

**Commands:**

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

**Expected:** every command exits 0; npm audit reports zero vulnerabilities; the requirements file maps every Phase 3A task ID to route, implementation path, test, command, and observed result; independent security and requirements reviews report no blocker.

## Commit checkpoints and seam serialization

1. A1 `refactor(app): share deterministic project assembly`
2. A2–A3 `feat(workbench): serve bounded real project brief`
3. A4 `fix(workbench): exchange launch nonce for memory bearer`
4. A5 `feat(workbench): render authenticated project brief`
5. B1–B2 `feat(workbench): add explore and canonical context views`
6. B3–B6 `feat(workbench): add knowledge timeline impact and safe mutations`
7. B7 `feat(workbench): render real knowledge views`
8. C1–C3 `feat(workbench): persist scoped events and cancellable jobs`
9. C4 `feat(workbench): reconcile live project state`
10. C5 `feat(workbench): export offline project snapshots`
11. D1–D2 `test(workbench): complete phase 3a acceptance`

The parent integrator, not a feature worker, owns each checkpoint commit/push and `web/dist/**`. Do not dispatch a later shared-seam task until the prior checkpoint is integrated and its focused gates pass.
