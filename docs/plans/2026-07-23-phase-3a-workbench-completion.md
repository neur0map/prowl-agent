# Phase 3A Completion Plan — Real Workbench

> **Execution contract:** Complete tasks in order. A worker owns only the files named by its assigned task unless a prerequisite forces a documented expansion. Every implementation task uses tests first, then implementation, then focused and integration verification, then review.

**Goal:** Turn the secured embedded shell at `95ba94f` into a real local knowledge workbench backed by the same deterministic project/query/context/knowledge services as CLI and MCP.

**Canonical contracts:**

- `docs/plans/2026-07-23-prowl-agent-product-evolution.md`
- `docs/plans/2026-07-23-hermes-inspired-agent-operations-port.md`
- `docs/plans/2026-07-23-product-evolution-handoff.md`

**Global invariants:** loopback IPv4 only; ephemeral bearer only in memory; exact Host/Origin/fetch-site gates; no browser storage; versioned bounded API; canonical server data; no copied domain logic in TypeScript; all long work cancellable; compiled-binary browser acceptance; no placeholder production data.

## Milestone A — One real connected fact

### Task A1: Deterministic project application assembly

**Files:**

- Create `internal/application/project.go`
- Create `internal/application/project_test.go`
- Modify `internal/cli/query.go`
- Modify focused CLI tests as required

**Contract:**

- `application.OpenProject(ctx, start, options)` resolves the workspace, opens the derived store, loads config, freshens the structural index, and assembles the query, knowledge, context, and capability services.
- The returned project has explicit `Close()` and no background goroutine.
- Configuration errors are returned; do not silently replace malformed config with defaults.
- Index refresh honors cancellation and preserves deterministic no-model operation.
- Refactor `openQuerier` to consume this assembly without changing CLI output.

**RED:** add tests for parent-directory resolution, malformed config, stale index refresh, cancellation, close behavior, and no-model operation.

**Verify:** focused package/CLI tests, full tagged Go tests, vet, and query output compatibility.

### Task A2: Versioned API envelope and real Brief service

**Depends on:** A1

**Files:**

- Create `internal/workbench/service.go`
- Create `internal/workbench/service_test.go`
- Modify `internal/workbench/api.go`
- Modify `internal/workbench/api_test.go`

**Contract:**

- Introduce a thin `Service` over `application.Project`.
- Add `GET /api/v1/brief` returning a typed, bounded snapshot assembled from real overview, knowledge health/counts, workspace identity, freshness, and capability metadata.
- Use stable envelope fields: `data`, `meta.request_id`, `meta.resource_version`; errors use `error.code`, `error.message`, and the same request ID.
- Support request-context cancellation and return no absolute home paths or unbounded source text.
- Keep `/api/v1/health` compatible unless a versioned field change is explicitly tested.

**RED:** unauthorized/Host/Origin tests plus success, empty project, malformed store, cancellation, path-redaction, deterministic ordering, and response-bound tests.

**Verify:** focused workbench tests, race test, full tagged Go suite, vet.

### Task A3: Wire `prowl-agent open` to a real workspace

**Depends on:** A1–A2

**Files:**

- Modify `internal/cli/open.go`
- Modify `internal/cli/open_test.go`

**Contract:**

- Resolve/open the project before starting the listener so setup failures do not expose a dead shell.
- Inject the workbench service into the handler and close it exactly once on every exit path.
- Preserve browser child reaping and graceful HTTP shutdown.
- Preserve `--no-browser`, ephemeral fragment bootstrap, and loopback origin. On successful automatic launch print only the redacted origin; for `--no-browser` or launch failure emit the full fragment once with a sensitive-value warning.

**RED:** missing workspace, malformed config, assembly error before listen, close on serve error/cancel, redacted successful-launch output, warned manual/failure bootstrap output, and a real Brief request through the command harness.

**Verify:** focused CLI race test plus full tagged suite/vet.

### Task A4: Frontend transport and Home/Brief screen

**Depends on:** A2–A3

**Files:**

- Create `web/src/lib/api.ts`
- Create `web/src/lib/api.test.ts`
- Create `web/src/features/brief/BriefPage.tsx`
- Create `web/src/features/brief/BriefPage.test.tsx`
- Modify `web/src/app/App.tsx`
- Modify `web/src/app/App.test.tsx`
- Modify styles only as needed

**Contract:**

- A single authenticated client reads the in-memory bootstrap token and sets bearer/JSON headers on same-origin `/api/v1/` requests only.
- Handle loading, empty, error/retry, and populated states without fake metrics.
- Show project purpose/map, subsystems/entry points/hotspots, knowledge health, freshness, and capability availability with progressive disclosure.
- Do not expose request IDs as primary UX, but make them copyable in errors.
- Preserve keyboard navigation, focus visibility, contrast, responsive layout, reduced motion, and no-storage invariants.

**RED:** token/header confinement, error envelope, cancellation, empty state, populated rendering, keyboard order, no local/session storage.

**Verify:** typecheck, unit tests, production build, axe/contrast checks.

### Task A5: Compiled-binary vertical acceptance

**Depends on:** A1–A4

**Files:**

- Modify `web/tests/workbench.spec.ts`
- Modify `web/playwright.config.ts` only if required
- Add/update a deterministic fixture under `testdata/`

**Contract:**

- Build the real web bundle and Go binary.
- Initialize/index a fixture project, launch `prowl-agent open --no-browser`, bootstrap through the fragment, and assert real Brief data.
- Verify direct unauthorized API rejection, hostile Host/Origin rejection, fragment removal, browser-history cleanup, no storage, refresh behavior, axe/contrast, keyboard traversal, and process shutdown.

**Verify:** compiled-binary Playwright run plus full Go/frontend gates.

**Milestone A exit:** one deterministic fact from a fixture is visible through CLI, MCP-compatible shared services, authenticated API, and the embedded browser without client-side duplication.

## Milestone B — Remaining read-only knowledge views

### Task B1: Explore hierarchy and guided tours

**Depends on:** Milestone A

- Add bounded cluster/file/symbol/source-preview queries with exact-line anchors.
- Add progressive hierarchy and three fixture-specific guided tours before force-graph mode.
- Test path rooting, source bounds, keyboard tree behavior, and no graph hairball default.

### Task B2: Context Lens parity

- Expose the canonical context-packet service directly through API DTOs.
- Add compact/standard/full budget controls and omission/provenance explanations.
- Golden-test byte/field parity with CLI and MCP structured content.

### Task B3: Impact, Knowledge, and Timeline

- Add impact summaries and exact evidence navigation.
- Add knowledge list/show/health/proposal diff routes and UI; acceptance/rejection remains transactional and reviewable.
- Add deterministic timeline items first; no inferred narrative without provenance.

### Task B4: Setup and diagnostics

- Reuse detect → plan → preview → apply → verify services.
- Expose configuration presence/status, never secret values.
- Require explicit approval and post-apply verification for writes.

## Milestone C — Jobs, live data, and export

### Task C1: Shared durable event/job substrate

- Add `internal/events` and `internal/jobs` with monotonic cursor, transactional durable records where required, bounded in-memory broker, cancellation, retention, and gap/reset semantics.
- No fixed sub-second idle polling and no unbounded subscriber/log queue.

### Task C2: Authenticated SSE

- Add bearer-authenticated fetch-streamed SSE for project/index/research/setup events.
- Resume from cursor, reconcile snapshots after reconnect, redact payloads, and terminate on server shutdown.

### Task C3: Frontend live-state client

- Treat events as invalidation, not truth; debounce canonical refetches.
- Cover offline/reconnect, scope change, gap/reset, slow consumer, cancellation, and stale optimistic version conflicts.

### Task C4: Single-file offline export

- Export a read-only bounded HTML snapshot with inline assets/data, provenance, generated-at/source-version metadata, no bearer, no active API calls, and CSP suitable for offline viewing.

## Milestone D — Phase 3A acceptance

- English and `en-XA` messages; no hardcoded UI prose outside locale catalogs.
- Three diverse fixture journeys.
- Visual regression at desktop/tablet/mobile.
- Keyboard, screen-reader, contrast, reduced-motion, zoom, and high-contrast checks.
- Go full tagged tests/vet/race; frontend typecheck/unit/build/audit; compiled-binary Chromium.
- Independent security review and independent requirements audit.
- Document every residual Phase 3A blocker; do not enter Phase 3B with placeholder data or duplicated service logic.

## Commit checkpoints

1. `refactor(app): share deterministic project assembly`
2. `feat(workbench): serve real project brief`
3. `feat(workbench): render authenticated project brief`
4. `feat(workbench): add explore context and impact views`
5. `feat(workbench): add knowledge timeline and setup views`
6. `feat(workbench): stream cancellable project jobs`
7. `feat(workbench): export offline project snapshots`
8. `test(workbench): complete phase 3a acceptance`

Each checkpoint is reviewed, tested, committed, and pushed before the next dependent slice.
