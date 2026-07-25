# C4 - Frontend live-state client report

## Scope

Implemented the authenticated C3 project-job event transport, canonical index-job status surface, App integration, and token-based responsive styling. The work changes only the C4-authorized source/test files plus this required report. No dependency, route, backend, generated `web/dist`, or C5+ change is included.

## RED → GREEN evidence

1. Wrote `web/src/transport/events.test.ts`, `web/src/features/jobs/JobStatus.test.tsx`, and the C4 App integration assertion before creating production event/status modules.
2. Ran the required C4 gate while the modules and App surface were absent. RED was observed: `events.test.ts` failed to resolve `./events`, `JobStatus.test.tsx` failed to resolve `./JobStatus`, and `App.test.tsx` could not find the `Index status` surface.
3. Implemented the smallest production transport, DTO validation, API error metadata, canonical job client, status component, App mount, and CSS needed for those tests.
4. A first GREEN candidate exposed an existing App-test compatibility regression: the idle persistent surface added a second `role="status"`, while the pre-existing brief test intentionally queried the single loading status. The root cause was the idle message being announced without a state change. The component now makes the idle message ordinary text and only exposes a polite live status after job/connection/action state exists.
5. The build then exposed that this TypeScript configuration's `ES2022` lib omits the declaration for the required native `Promise.withResolvers()`. The transport retains the native API and uses a narrow local structural type for its resolver result; no target/config/dependency broadening was needed.
6. During the required self-review, invalid refresh/cancel DTO callbacks were found to throw inside a promise fulfillment callback, leaving an unhandled rejection. Added a RED test for invalid refresh DTOs; it failed with the expected unhandled `job response was invalid` and retained “Starting index refresh…”. The GREEN handler now retains no invalid payload and reports `Index response could not be verified.`

Final required command and observed result:

```sh
cd web && npm test -- --run src/transport/events.test.ts src/features/jobs/JobStatus.test.tsx src/app/App.test.tsx && npm run build
```

- Vitest: **3 files passed, 28 tests passed**.
- TypeScript typecheck passed as part of `npm run build`.
- Vite production build passed: **21 modules transformed**.

## Stream and state self-review

- `streamProjectJobEvents` calls only C3 `apiFetch`, which adds the in-memory Authorization bearer to same-origin `/api/v1/` requests. The stream request has exact explicit `stream_scope`, `scope_id`, `epoch`, and `sequence` query fields plus `Accept: text/event-stream`; bearer data never enters the URL.
- The parser uses `ReadableStream<Uint8Array>` and `TextDecoder`, handles LF/CRLF and chunk boundaries, ignores comment keepalives, accepts only a single `event:` plus single-line JSON `data:` record, and emits only validated `invalidate` or `reset` notifications. Missing/non-SSE content type, no body, malformed JSON, wrong event shape, wrong scope, invalid cursor, or unsafe snapshot URI fail the stream before any UI state can use data.
- Runtime validation requires lower-hex C3 job/scope identifiers, safe bounded job text, positive safe version/epoch, nonnegative safe cursor sequence, 0–100 integer progress, exact project-job cursor scope, and bounded terminal metadata. Invalid canonical DTOs are not rendered.
- Stream cancellation cancels the reader, releases its lock, and preserves abort semantics. The reconnect loop owns retry policy outside the parser, updates its cursor before forwarding each notification (including reset replacement), has abortable 250 ms to 4 s exponential delay, and exposes only transient `connecting`, `live`, `retrying`, or `offline` state.
- Job status never reduces stream payload into durable UI job truth. Notifications, reset, retry/offline transition, and reconnect schedule a 200 ms canonical GET. The request coordinator permits one in-flight request plus one trailing request. Terminal canonical GET calls `onInvalidate`; the App increments its existing reload key, preserving hash routing and main-focus recovery.
- Refresh is click-only; mount never POSTs. Cancel uses the displayed canonical version and a fresh cryptographic printable UUID per click. `409 job_version_conflict` immediately schedules canonical GET instead of changing a version locally. Other network failures retain the last canonical job and announce retry availability.
- Unmount, watcher replacement, job terminal transition, and action replacement abort their controllers; terminal/idle jobs do not watch or poll.

## UI, accessibility, and responsive self-review

- `JobStatus` is a persistent semantic section with a text label, native keyboard-operable buttons, visible global focus styling, a labelled native `<progress>`, text percentage, state text in addition to token-colored border treatment, and a polite non-blocking message once it has something to announce.
- Idle presents exactly one `Refresh index` action. Active jobs show status, phase, progress, and cancel. `cancelling` renders a disabled `Cancelling index…` control. Terminal jobs give bounded safe textual outcomes and a future `Refresh index` action.
- The App mounts the status immediately inside `<main>` before the routed content and omits it only in the existing session-security error branch.
- Styling uses the existing `--surface-raised`, `--border`, `--accent`, `--accent-hover`, `--secondary`, and `--muted` tokens; new spacing values follow the existing 4 px grid. At the existing narrow breakpoint, the surface/action row stacks. No new animation or reduced-motion override was introduced.
- Confirmed no production `EventSource` reference.

## Concerns

None. The transport intentionally surfaces only a bounded generic verification/retry message; raw server response text and bearer values remain unavailable to the UI.

## Post-review transport remediation

An independent review found two portability and concurrency defects after the first C4 commit:

1. The `TextDecoder` instance was module-global. Streaming decoder state is mutable, so two overlapping stream readers could corrupt each other's partial UTF-8 input.
2. `abortableDelay` called `Promise.withResolvers()` behind a TypeScript cast. The configured `ES2022` target does not declare it, and older supported Chromium implementations do not provide it.

RED evidence was added before the remediation:

- A concurrent stream test fed one reader a partial UTF-8 comment and then fed a second reader a valid keepalive plus change event. It failed with `event payload was invalid`, proving shared decoder state crossed readers.
- A delay test removed `Promise.withResolvers` from the runtime, then tested both timeout and abort settlement. It failed with `TypeError: Promise.withResolvers is not a function`.

GREEN remediation:

- `parseEventStream` now constructs one `TextDecoder` per reader.
- `abortableDelay` now uses the standard promise constructor with one cleanup function shared by timeout and abort. It clears the timer, unregisters the abort listener, and resolves only once.

The exact C4 gate was rerun after remediation:

```sh
cd web && npm test -- --run src/transport/events.test.ts src/features/jobs/JobStatus.test.tsx src/app/App.test.tsx && npm run build
```

Observed result: 3 Vitest files passed, 30 tests passed, TypeScript typecheck passed, and Vite built 21 modules successfully.
