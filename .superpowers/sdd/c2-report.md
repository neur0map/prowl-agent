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

## Final post-commit verification
The first post-commit gate exposed a missing persisted `index.ProgressReporter` type used by the application entrypoint. The root cause was a stale edit snapshot; the type was added through the structural editor. Re-ran successfully:

```text
CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/jobs ./internal/events ./internal/application ./internal/index ./internal/cli -run 'Test(Job|JobsDBPath|OutboxTransaction|CommitBeforePublish|PublisherWatermark|StartupRefreshJob|OpenStartupJobWiring|Cancel|Restart)' -count=1
go test: 5 packages ok

CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/events -run 'Test(CursorScope|AdapterConformance|ConnectedSubscriberSweep|RetentionReset|SlowSubscriber)' -count=1
go test: 1 packages ok
```

## Final remediation: progress and migration

### RED to GREEN proof
1. RED:
   ```text
   CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/index -run '^TestIndexWithProgressReportsBoundedPhases$' -count=1
   --- FAIL: TestIndexWithProgressReportsBoundedPhases
   progress snapshots=[{starting 0} {complete 100}], want walking, indexing, resolving, complete
   ```
   GREEN:
   ```text
   CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/index ./internal/application ./internal/jobs ./internal/cli -run 'Test(IndexWithProgress|RefreshWithProgress|ProjectRefreshRunner|JobsMigration)' -count=1
   go test: 4 packages ok
   ```
2. RED:
   ```text
   CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/application -run '^TestRefreshWithProgressForwardsIndexPhases$' -count=1
   --- FAIL: TestRefreshWithProgressForwardsIndexPhases
   progress=[{refreshing 0} {complete 100}] missing "walking"
   ```
   GREEN: included in the four-package focused command above.
3. RED:
   ```text
   CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/cli -run '^TestProjectRefreshRunnerForwardsRealIndexProgressToJobCallback$' -count=1
   internal/cli/open_test.go:427:9: undefined: projectRefreshRunner
   FAIL github.com/prowl-agent/prowl-agent/internal/cli [build failed]
   ```
   GREEN:
   ```text
   CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/cli -run '^TestProjectRefreshRunnerForwardsRealIndexProgressToJobCallback$' -count=1
   go test: 1 packages ok
   ```
4. The v0 fixture migration test was written before the migration refactor. It was already behaviorally green against the prior implementation because it created the same initial empty `jobs_schema` table in Go; the remediation removes that duplicate schema creation and makes the embedded migration artifact the sole creation authority. `TestJobsMigrationUpgradesV0Artifact` now executes `testdata/v0.sql`, opens through `jobs.Open`, verifies all v1 authority/jobs/outbox/index objects, and enqueues a job. `TestJobsMigrationRefusesFutureSchema` verifies the version remains `2` and no tables are added after refusal.

### Changed behavior
- `index.ProgressReporter` now returns an error. Indexing invalidates `index_state` before the first `walking:0` callback, reports `indexing` after traversal and at most once per 32 candidates plus its final `90`, reports `resolving:95` before graph resolution, and reports `complete:100` only after index metadata is complete. A callback failure is returned; a final callback failure restores `index_state=incomplete`.
- `Project.Refresh` and `RefreshWithProgress` share the same refresh gate, file-lock, retry, generation, incomplete-state, and close-check path. The progress form uses `IndexWithOptionsProgressContext`; the non-progress form uses the nil-reporter path.
- The concrete CLI jobs runner forwards each real index snapshot to the durable jobs progress callback and propagates its errors. Its only pre-index snapshot remains `refreshing:1`.
- `jobs.migrate` detects `jobs_schema` without constructing it in Go, refuses future versions before applying migrations, and executes the embedded v1 artifact atomically for new or v0-empty databases. The v1 fixture now includes the v1 progress check and active-index unique index.

### Exact final verification
```text
CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/jobs ./internal/events ./internal/application ./internal/index ./internal/cli -run 'Test(Job|JobsDBPath|OutboxTransaction|CommitBeforePublish|PublisherWatermark|StartupRefreshJob|OpenStartupJobWiring|Cancel|Restart|IndexWithProgress|Migration)' -count=1
go test: 5 packages ok

CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/events -run 'Test(CursorScope|AdapterConformance|ConnectedSubscriberSweep|RetentionReset|SlowSubscriber)' -count=1
go test: 1 packages ok
```

### Self-review
- The index callback sequence is phase-ordered, bounded, and nondecreasing; its tests exercise 65 files so periodic reporting and the final report are distinct.
- Progress callback errors are never discarded: index returns them, application returns the index error through its existing incomplete-generation handling, and the CLI runner returns the durable jobs callback error.
- Migration checks the stored version before the artifact is invoked, so a future database remains untouched.
