# C3 - authenticated SSE and job routes

## Scope and composition

C3 adds `internal/workbench/events.go` and `internal/workbench/jobs.go`, plus focused tests. `cli` composes the single C2 `jobs.Service` and its C1 `events.Broker`, attaches the jobs service to `application.Project` (the existing lifecycle owner), then atomically attaches those same borrowed objects to `workbench.Service`. Workbench never closes either resource. `Project.Close -> jobs.Service.Close` remains the sole close path.

`workbench.Service.AttachLiveOperations` publishes a single immutable pair under an RW lock. Readers either receive both dependencies or the typed `ErrLiveOperationsUnavailable`; partial attachment is impossible. `jobs.Service.StreamState` is the narrow read-only C2 seam used to form safe job cursors.

## RED → GREEN evidence

1. **Job routes:** Added `TestJobRoutesRefreshGetCancelAndIdempotency` using a real `jobs.Store`, `jobs.Service`, and C1 `events.Broker`. Initial focused run failed to compile because `AttachLiveOperations` and the bounded job DTO did not exist. Implemented the pair attachment, safe DTO validation, canonical refresh/get/cancel routes, C2 error mapping, and a synchronized 256-entry FIFO idempotency cache. Focused test then passed.
2. **SSE:** Added `TestSSEEmitsRedactedReplayAndUnsubscribesOnDisconnect` against the same real C2/C1 topology. It initially lacked the live stream API. Implemented exact cursor parsing, broker subscription, bounded redacted `project-job.changed` events, reset formatting, headers, per-`Next` keepalive deadline, and cancellation/close handling. Focused test passed.
3. **CLI injection:** Extended the existing production composition test with an authenticated refresh request. It failed with `503 service_unavailable`, proving CLI had not attached C2 live operations to workbench. Changed `newProjectJobsService` to return the same broker together with its jobs service and attached that pair before handler creation. The test passed.
4. **No-body refresh:** Added an unknown-length (`ContentLength=-1`) refresh body test. It failed with `202`, proving the earlier content-length-only guard admitted chunked bodies. The route now reads at most one byte and rejects every non-empty body; the test passed.
5. **Race verification:** The required race gate first found a race in the test itself: the test read `httptest.ResponseRecorder` concurrently with the SSE goroutine writing it. Replaced polling with a write notification and only inspected the recorder after cancellation joined the handler. The required race gate then passed.

## Security and behavior self-review

- Authentication/route boundary: all four routes are behind the existing Host, Origin, `Sec-Fetch-Site`, and bearer checks. Query credentials are rejected case-insensitively (`access_token`, `token`, `bearer`, `authorization`, `api_key`) before route dispatch/auth work; responses do not echo values.
- Cursor parsing: SSE accepts exactly the four required singleton keys, rejects unknown/duplicate/missing values, leading-zero/over-bound numeric encodings, zero epoch, invalid scope, and does not inspect `Last-Event-ID`.
- Stream lifecycle: flusher capability is checked before headers; subscription close is deferred; every `Next` has a deadline; disconnect and broker close return silently after SSE headers, without a JSON error.
- Redaction: delivery data contains only a C1 cursor and stable invalidation kind. C2 payload, job ID, evidence, source content, workspace path, store path, nonce, bearer, and raw errors are not streamed. Reset uses a fixed bounded opaque snapshot URI. Job routes serialize a field-validated DTO rather than `jobs.Job` and omit evidence.
- Idempotency: the cache is synchronized, bounded to 256 FIFO entries, and its lock spans lookup, mutation, and recording so simultaneous same-key cancellations cannot execute a second mutation. Reusing a key with different job/version returns `409 idempotency_conflict`.
- C2 lifecycle: no schema/worker semantics changed. The only jobs change is read-only `StreamState`; workbench borrows C2 resources and CLI attaches the exact broker used by C2.

## Required verification

Passed exactly:

```sh
CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/workbench ./internal/events ./internal/jobs -run 'Test(SSE|JobRoutes|Reconnect|Shutdown|Redaction)' -count=1
# go test: 3 packages ok

CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/workbench ./internal/cli -count=1
# go test: 2 packages ok
```

## Remediation RED -> GREEN

1. `TestSSEResetPreservesSafeC1SnapshotURI` initially observed the invented `/api/v1/jobs/snapshot` value. Reset delivery now preserves the C1 URI only when it is a bounded opaque `snapshot://<safe-id>` value; unsafe paths, queries, credentials, and fragments fail closed. C2 fills an otherwise-empty initial snapshot URI with the stable opaque project scope URI, so normal scope/epoch mismatch recovery still emits a reset without a storage path.
2. `TestAPISecurityRejectsMalformedCredentialQuery` initially reached health handling with `503` for `authorization=secret;...`. The boundary now calls `url.ParseQuery` on `RawQuery`, rejects parsing errors before any query-key inspection or route/auth work, and never reflects the value.
3. The job-route idempotency test now covers a missing-job `404` replay and key reuse against a different job. The cache records the validated request signature and either a bounded success DTO or a canonical error outcome while holding its mutex across mutation. Replays cannot perform a second mutation and mismatched reuse returns `409`.
4. `TestServeWorkbenchHTTPCancelsActiveSSEOnShutdown` initially timed out because `http.Server.Shutdown` waited on a live stream context. `serveWorkbenchHTTP` now uses a cancelable `BaseContext`, cancels active requests before `Shutdown`, and the production SSE test passes.
5. `TestGetMissingJobMapsUnknown` and job-route coverage verify valid unknown IDs map to `404 job_not_found`. `Store.Get` and the new read-only `Store.Snapshot` map `sql.ErrNoRows` to `ErrUnknownJob`.
6. `TestSnapshotReadsJobAndOutboxHeadAtomically` and race-tested `TestSnapshotDoesNotPairJobWithInterveningHead` establish the C2 read transaction contract. Workbench GET, refresh, and cancel render DTOs from one job-and-head snapshot, rather than separately reading a job then stream state.

## Remediation self-review

- Reset URIs are bounded, opaque, no-query `snapshot` URIs with a safe identifier host. A raw filesystem path or credential-bearing URI is rejected before a partial SSE frame is written.
- Query parsing happens before authorization credential-key detection and before dispatch, including malformed semicolon query forms.
- Replay cache entries contain only validated request identity plus already-safe outcome fields; FIFO eviction remains bounded to 256.
- Server shutdown first cancels `BaseContext`, which terminates SSE `Next` calls, then invokes `Shutdown`; ownership of C2 jobs/broker remains unchanged.
- The C2 snapshot addition is read-only and uses no schema migration. Its SQLite transaction establishes one consistent job/head view.

## Reset fallback remediation

`TestSSEResetUsesCanonicalFallbackForEmptyC1SnapshotURI` was RED because an empty C1 reset URI was rejected and the stream exited after its headers. `safeSnapshotURI` now maps only the empty case to the bounded opaque canonical `snapshot://project-job`, while nonempty values still undergo the existing strict `snapshot://<safe-id>` validation. `TestSSEInitialAuthorityScopeMismatchEmitsReset` uses an actual C2 jobs store and C1 broker at initial authority state, requests a mismatched scope, and verifies a reset frame with a snapshot URI before cancellation. This keeps reset recovery available without accepting or reflecting a raw path, query, fragment, or credential.

## Cancel availability remediation

`TestJobCancelUnavailableIsPrivacySafe` was RED with a nil-pointer panic because cancel accessed the replay cache before resolving live operations. Cancel now obtains the complete live pair first and returns the normal privacy-safe `503 service_unavailable` for both `Service:nil` and an unattached service. Replay cache access begins only after that boundary succeeds.
