# C2 implementation report

## Scope
- Added `internal/jobs` durable SQLite authority, migration artifacts, durable outbox state, service, cancellation/restart tests, and focused migration/path tests.
- Added `internal/events/project_jobs.go` C1 `Outbox` adapter plus bounded replay test.
- Added bounded `internal/index` progress API and focused progress test.
- Updated project lifecycle/startup state and CLI durable-job composition/listen-before-worker handoff.

## RED → GREEN evidence
1. `go test ./internal/jobs -run 'TestJobsDBPathUsesPrivateXDGProjectDirectory|TestJobEnqueueAndCancelAreDurable' -count=1`
   - RED: undefined `DBPath`, `Open`, and `StatusCancelling`.
   - GREEN: package passed after adding the jobs model/store and SQLite migration.
2. `go test ./internal/jobs -run TestJobServiceCancelsRunningJob -count=1`
   - RED: undefined `NewService`.
   - GREEN: passed after adding the cancellable worker, post-commit publisher call, durable cancellation, and terminal cancellation persistence.
3. `go test ./internal/events -run TestProjectJobsOutboxReplaysBoundedDurableEvents -count=1`
   - RED: undefined `NewProjectJobsOutbox`.
   - GREEN: passed after adding the durable C1 adapter.
4. `go test ./internal/index -run TestIndexWithProgressReportsBoundedSnapshots -count=1`
   - RED checkpoint recorded for missing progress symbols; harness output incorrectly reported success despite missing symbols and was reported through `xd://report_issue`.
   - GREEN: passed after adding `Progress`, `ProgressReporter`, and `IndexWithOptionsProgressContext`.
5. Startup pending test was added before implementation. The harness likewise returned an anomalous green result despite the missing method; this was reported. Integration compilation later exercised the real symbols successfully.

## Transaction and lifecycle semantics
- Jobs use a separate XDG data database at `prowl-agent/projects/<sha256(abs Workspace.Root)>/jobs.db`, with private parent/database modes, WAL, foreign keys, busy timeout, schema identity/versioning, and future-version refusal.
- Enqueue, claim, progress, cancel, reconciliation, and terminal outcomes append a redacted `project-job.changed` row in the same SQL transaction as the job mutation.
- The durable adapter maps jobs authority epoch/sequence into C1 `project-job` cursors; replay is limited to `limit + 1`, preserves `More`, supports retention reset, and stores a monotonic publisher watermark.
- Service cancellation cancels its active runner context; a cancellation resolves as `cancelled`, not `failed`. Startup reconciliation requeues interrupted running index jobs and terminalizes requested cancellation.
- CLI builds/enqueues the service after a usable project is obtained and starts it only after local listener/handler construction. Project close stops the attached service before its index store.

## Exact verification output
```text
CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/jobs ./internal/events ./internal/application ./internal/index ./internal/cli -run 'Test(Job|JobsDBPath|OutboxTransaction|CommitBeforePublish|PublisherWatermark|StartupRefreshJob|OpenStartupJobWiring|Cancel|Restart)' -count=1
go test: 5 packages ok

CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/events -run 'Test(CursorScope|AdapterConformance|ConnectedSubscriberSweep|RetentionReset|SlowSubscriber)' -count=1
go test: 1 packages ok
```

## Self-review and concerns
- Reviewed package direction: jobs does not import application/events/cli/index; the events adapter imports jobs; CLI composes the broker/adapter/service.
- Reviewed event payload: it includes only opaque job ID, version, status, phase, bounded progress, outcome, and safe error code.
- Reviewed SQL/outbox mutation ordering and post-commit publisher behavior.
- Concern: the harness intermittently served stale `read` snapshots and false-positive `go test` results for newly added undefined symbols. These tool inconsistencies were reported immediately; final race gates are the successful verification evidence above.
