# Task D2 report -- final compiled-binary requirements and security audit

## Status

`DONE_WITH_CONCERNS`

The focused automated D2 checkpoint is complete. No known product defect remains
in the focused oracles. Phase 3A is not accepted: participant sessions,
independent security/requirements reviews, and the controller-owned full gates
remain pending.

Base: accepted D1 commit `43e4f3f` on branch `prowl-full-evolution`.
The checkpoint commit is the commit containing this report; its SHA is returned
to the controller rather than self-referenced in committed content.

## Requirements sources reviewed

- `.superpowers/sdd/2026-07-23-phase-3a-workbench-completion/task-D2-brief.md`
- `docs/plans/2026-07-23-phase-3a-workbench-completion.md`
- `docs/verification/full-roadmap-ledger.md`
- `.superpowers/sdd/d1-report.md`
- `web/e2e/{support,workbench,accessibility,fixtures}.spec.ts`
- `internal/workbench/api.go` declared route inventory
- production App route/screen composition and transport clients

The route evidence is based on explicit method/path correspondence and real
responses. No conclusion is inferred from a route count.

## Regression-first repairs

### Blocker 1 -- guided tours contained unrooted steps

Compiled RED, before production repair:

```text
cd web
npx playwright test e2e/workbench.spec.ts --grep 'guided tour:'
3 failed

go-auth-service: guided step 3 must be rooted in source evidence
expected > 0, received 0

ts-checkout: guided step 3 must be rooted in source evidence
expected > 0, received 0

rice-config: guided step 2 must be rooted in source evidence
expected > 0, received 0
```

Source-level RED added before the repair:

```text
go test -tags sqlite_fts5 ./internal/workbench \
  -run 'TestExploreProjectsDeterministicSectionsAndTours' -count=1
FAIL: tour "onboarding" step 2 is not rooted in source evidence
```

Root cause: tours reused five high-level Explore sections, but subsystem and
capability facts have no source anchor, and fixtures can also have an empty
entrypoint or hotspot section. The smallest source repair builds bounded tour
steps from deterministic indexed-file/chunk evidence under the project read
guard. It emits 5–12 steps, uses actual project-relative line anchors, validates
derived paths, and retains the existing three stable tour IDs. No fallback URL
or client-side fabricated anchor was added.

GREEN:

```text
go test -tags sqlite_fts5 ./internal/workbench \
  -run 'TestExploreProjectsDeterministicSectionsAndTours|TestGuidedTourRejectsUnknownID|TestExploreAndTourAPIRoutesRequireBoundedReadRequests' \
  -count=1
go test: 1 package ok

cd web
npx playwright test e2e/workbench.spec.ts --grep 'guided tour:'
3 passed
```

The final fixture journey subsumes the original focused test and verifies each
anchor by navigating the production hash route and observing a visible, non-error
source preview from the compiled binary.

### Blocker 2 -- real Impact 200 response failed frontend validation

Compiled RED after guided tours were green:

```text
npx playwright test e2e/workbench.spec.ts \
  --grep 'fixture journey: go-auth-service'
FAIL: expected visible heading "Impact: README.md"
actual UI: "Impact evidence unavailable. Try another source path."
```

The real API returned 200, but empty `blast.by_subsystem` and
`blast.direct_files` serialized as `null`. The strict frontend runtime validator
correctly rejected them. A backend projection regression was added first:

```text
go test -tags sqlite_fts5 ./internal/workbench \
  -run 'TestImpactNormalizesEmptyRelationshipCollections' -count=1
FAIL: empty blast collections must be canonical arrays
```

Root repair: normalize the two server-owned empty blast collections to `[]`
immediately after the canonical query projection. Frontend validation was not
weakened and malformed data is still rejected.

GREEN:

```text
go test -tags sqlite_fts5 ./internal/workbench -run 'TestImpact' -count=1
go test: 1 package ok

cd web
npx playwright test e2e/workbench.spec.ts --grep 'fixture journey:'
3 passed
```

During harness calibration, assertions were corrected to the documented wire
contracts (variable-width hexadecimal index resource versions, SHA-256 setup
resource versions, `events` for Timeline, `project-job.changed` SSE names, and a
scope-derived snapshot URI). These were test-assumption corrections, not product
repairs or weakened security assertions.

## Final compiled fixture journeys

Every journey used a copied fixed fixture, a freshly built real Go binary, two
noninteractive initialization passes, `open --no-browser --port 0`, the
mode-0600 handoff, the committed frontend bundle, and same-origin authenticated
production requests.

| Fixture | Guided evidence | Screen/data route sequence | Result |
| --- | --- | --- | --- |
| `go-auth-service` | 5 rooted visible steps; every anchor opened | Brief → Explore → onboarding tour → all source previews → Context search `access tokens` → selected Context → Impact → Knowledge → Timeline → Setup | Passed |
| `ts-checkout` | 5 rooted visible steps; every anchor opened | Same sequence; Context search `checkout calculation` | Passed |
| `rice-config` | 5 rooted visible steps; every anchor opened | Same sequence; Context search `Hyprland` | Passed |

Each fixture had to observe these explicit inventory correspondences:

```text
GET  /api/v1/brief
GET  /api/v1/explore
GET  /api/v1/tours/{tour_id}
GET  /api/v1/source
POST /api/v1/context/search
POST /api/v1/context/get
POST /api/v1/impact
GET  /api/v1/knowledge
GET  /api/v1/timeline
GET  /api/v1/setup/detect
```

The assertions also require fixture identity or fixture-derived paths, a real
resource version, populated Context/Impact where applicable, canonical real
empty arrays for fixture-backed empty screens, and visible rendered headings.

## Security and recovery coverage

The final compiled E2E covers:

- unauthorized API denial;
- hostile exact Host denial;
- hostile Origin denial;
- hostile `Sec-Fetch-Site: cross-site` denial;
- nonce replay denial;
- real 60-second nonce expiry denial;
- nonce absence from ordinary stdout, stderr, `/proc/<pid>/cmdline`, and
  `/proc/<pid>/environ`, asserted as booleans without reporting the value;
- mode-0600 one-time handoff;
- clean browser fragment/query/history plus empty cookie/local/session storage;
- scoped cursor replay from sequence 0;
- resume from the delivered tuple with increasing sequence in the same
  scope/epoch;
- hostile scope reset to the server-authoritative scope and snapshot URI;
- a compiled active-index cancellation against a generated bounded workload;
- offline export with networking blocked and CSP `default-src`, `connect-src`,
  and `script-src` set to `'none'`;
- exported absence of nonce, bearer, authorization prose, API paths, and private
  temporary absolute paths without emitting secret values;
- production frontend scan rejecting `placeholder|TODO metric|Math.random`.

Commit-before-publish repair for an already-connected subscriber is exercised by
the final focused `internal/events` race gate. The compiled stream additionally
proves replay/resume/reset over the real HTTP/SSE boundary.

## Focused final gates

Environment: local Linux, `CGO_ENABLED=1`, SQLite FTS5, repository Node
installation, and repository-pinned Playwright Chromium.

```text
go test -tags sqlite_fts5 ./internal/context ./internal/cli ./internal/mcp ./internal/workbench \
  -run 'TestCanonicalContextProjection|TestContextLensParity' -count=1
go test: 4 packages ok
```

Canonical parity excludes only `trace_id`, request IDs, transport envelope
fields, and explicitly tagged timestamps. IDs/order, summaries, budgets,
omissions, provenance, citations, and freshness remain compared.

```text
go test -race -tags sqlite_fts5 ./internal/workbench ./internal/cli \
  -run 'Test(Bootstrap|Nonce|OpenOutput|BrowserLaunch|RouteInventory|RequestBounds|PrincipalDerivation|MutationAuth|Explore|GuidedTour|SourcePreview|Impact|SSE|JobRoutes|Reconnect|Shutdown|Redaction|OfflineExport)' \
  -count=1
go test: 2 packages ok
```

```text
go test -race -tags sqlite_fts5 ./internal/events ./internal/jobs \
  -run 'Test(CursorScope|AdapterConformance|ConnectedSubscriberSweep|RetentionReset|SlowSubscriber|Job|OutboxTransaction|CommitBeforePublish|PublisherWatermark|Cancel|Restart)' \
  -count=1
go test: 2 packages ok
```

```text
cd web
npx playwright test e2e/workbench.spec.ts
7 passed (1.1m)
```

Final focused cleanup checks:

```text
go test -race -tags sqlite_fts5 ./internal/workbench \
  -run 'Test(Explore|GuidedTour|SourcePreview|Impact)' -count=1
go test: 1 package ok

cd web && npm run typecheck
passed, no diagnostics

git diff --check -- web/e2e/workbench.spec.ts internal/workbench/explore.go internal/workbench/explore_test.go internal/workbench/impact.go internal/workbench/impact_test.go docs/verification/phase-3a-requirements.md .superpowers/sdd/2026-07-23-phase-3a-workbench-completion/task-D2-report.md
no output, exit 0
```

No frontend source changed. `git status --short` showed no `web/dist/**` change;
the controller still owns the final exact bundle-diff gate.

## Requirements evidence

Created `docs/verification/phase-3a-requirements.md` with:

- an A1–D2 row for every Phase 3A task;
- explicit route/surface, implementation path, oracle, command/result,
  fixture/environment, exclusions/thresholds, migration/restart/rollback, and
  review disposition;
- a strict historical-versus-final-tree distinction;
- the closed canonical Context volatility allowlist;
- exact fixture journeys and route correspondence;
- exact five-question newcomer rubric;
- 45 blank `--` score rows for P1–P3 × three fixtures × five questions;
- explicit pending participant totals and pending independent security and
  requirements review dispositions;
- the exact controller-owned full gates.

No participant answer, score, duration, reviewer identity, review result,
project-wide command, npm audit result, or bundle result was invented.

## Changed paths

- `web/e2e/workbench.spec.ts`
- `internal/workbench/explore.go`
- `internal/workbench/explore_test.go`
- `internal/workbench/impact.go`
- `internal/workbench/impact_test.go`
- `docs/verification/phase-3a-requirements.md`
- `.superpowers/sdd/2026-07-23-phase-3a-workbench-completion/task-D2-report.md`

No frontend source changed, so `web/dist/**` was not regenerated.

## Self-review

- Production changes are limited to the two defects reproduced by compiled D2
  assertions and their backend regressions.
- Guided steps come from indexed project evidence under the read guard; they are
  bounded, deterministic, rooted, and path-validated.
- Impact canonicalization occurs at the backend projection boundary; the client
  remains fail-closed.
- Browser assertions use real compiled routes, not production mocks, route
  counts, or copied domain logic.
- Credential checks reduce secret presence to booleans; credentials are absent
  from reports and committed artifacts.
- No frontend source, dependency, migration, route, token policy, cursor model,
  knowledge authority, or product feature was added.

## Concerns and precise remaining controller work

No focused product defect remains. The following are the only known completion
concerns:

1. Run the exact project-wide Go/vet/race/npm/audit/build/bundle/whitespace gates
   in `docs/verification/phase-3a-requirements.md` on the committed checkpoint.
2. Run three independent participants through all three fixture journeys, record
   all 45 real binary-scored answers and times, and evaluate ≥36/45 overall plus
   ≥4/5 for each participant-fixture journey within 10 minutes.
3. Obtain independent security and requirements reviews of the committed D2
   checkpoint and record real dispositions.

Until all three are complete, D2 remains an automated/evidence checkpoint and
Phase 3A must not be marked accepted.

## Review fix round 1

The task review produced eight open findings. All eight were addressed on top of
`f6ecf39`.

### Tour bounds, cancellation, and distinct evidence

The recovered partial fix introduced `Store.FirstChunksContext(ctx, limit)`.
Its SQL joins `files` to the first `chunks` row before applying the deterministic
file-path limit, so chunkless files cannot consume the bound. The query uses
`QueryContext`; request cancellation reaches a blocked SQLite read. Tour facts
use distinct file/line anchors from those chunks and additional distinct lines
only when needed. Fewer than five distinct anchors returns no tour and
`ErrTourNotFound`; no source step is copied or relabeled as padding.

The original worker became unavailable after writing the tests and partial
implementation, so no direct RED console transcript survived. The recovered
tests explicitly cover twelve alphabetically earlier chunkless files,
pre-cancelled context, duplicate-anchor rejection, and insufficient evidence.
The controller observed:

```text
go test -race -tags sqlite_fts5 ./internal/store ./internal/workbench \
  -run 'Test(FirstChunksContext|ExploreFiltersChunkless|ExploreDoesNotPad|ExploreProjectsDeterministic|Impact)' \
  -count=1
go test: 2 packages ok
```

Compiled fixture tours remained rooted and visible:

```text
cd web
npx playwright test e2e/workbench.spec.ts --grep 'fixture journey'
3 passed (4.6s)
```

### Expiry redaction and terminal cancellation

The expiry oracle now attaches a separate 64 KiB bounded stdout/stderr collector
before startup and keeps it attached through the expired bootstrap request and
process `close`. Truncation fails the test. Nonce shape and absence assertions
operate on booleans, so a failing matcher cannot print the nonce.

The export/cancellation oracle also validates nonce/bearer shape and export
absence through booleans. Cancellation now refetches the current version and
retries only version conflicts with a fresh idempotency key. After a successful
request or observed `cancelling` state, `expect.poll` refetches the same job until
its terminal state is exactly `cancelled`; transitional acceptance alone cannot
pass.

```text
cd web
npx playwright test e2e/workbench.spec.ts --grep 'expired nonce|events jobs'
2 passed (1.1m)
```

The only output noise was the workstation-provided Node warning that `NO_COLOR`
is ignored because `FORCE_COLOR` is set. No application warning or secret value
was printed.

### Evidence corrections

- The C4 row now credits F-BROWSER only for compiled HTTP/SSE server replay,
  resume, reset, and terminal cancellation. Production `events.ts` and Job
  Status recovery remain historical and pending final review.
- The cleanup evidence now records the literal command:

```text
git diff --check -- web/e2e/workbench.spec.ts internal/workbench/explore.go internal/workbench/explore_test.go internal/workbench/impact.go internal/workbench/impact_test.go docs/verification/phase-3a-requirements.md .superpowers/sdd/2026-07-23-phase-3a-workbench-completion/task-D2-report.md
no output, exit 0
```

### Fix-round changed paths

- `internal/store/context.go`
- `internal/store/queries_test.go`
- `internal/workbench/explore.go`
- `internal/workbench/explore_test.go`
- `web/e2e/workbench.spec.ts`
- `docs/verification/phase-3a-requirements.md`
- `.superpowers/sdd/2026-07-23-phase-3a-workbench-completion/task-D2-report.md`

TypeScript typecheck and gopls diagnostics for the amended Go store file were
clean. Participant sessions, final independent reviews, and controller full
gates on the post-fix commit remain pending.
