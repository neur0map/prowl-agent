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
