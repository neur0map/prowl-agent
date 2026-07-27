# Task B0.3 - Resumable session ledger and CLI service - Report

## Summary
Implemented the minimal durable session repository/service over the single B0.1
operations database and a thin CLI adapter. A session is created pinning the
exact immutable B0.2 snapshot + exposure canonical bytes, one turn trajectory is
appended transactionally with its operations outbox event, the ledger reopens
after process restart, and the pinned snapshot/exposure are returned without
re-resolving mutable state. No provider execution, HTTP route binding, streams,
FTS, branching, export, or repair - those remain later tasks.

## Changed paths
Created (exact ownership):
- `internal/session/model.go` - status constants, request/response DTOs (no actor
  fields), `Service` interface, strict actor-rejecting JSON decoders, validation.
- `internal/session/repository.go` - all SQL over the existing B0.1
  `sessions`/`session_turns`/`session_entries` tables via `operations.Tx`/`ReadTx`.
- `internal/session/service.go` - transactional create/append/get/exposure logic.
- `internal/session/repository_test.go` - repository-level behavior.
- `internal/session/service_test.go` - service behavior + fixture round-trip.
- `internal/session/testdata/session_v1.json` - hand-authored compatibility fixture.
- `internal/cli/session.go` - `newSessionCmd()` with `start|turn|show|resume`.
- `internal/cli/session_test.go` - CLI end-to-end against a real temp service.

Modified (narrow, only):
- `internal/cli/cli.go` - registered `newSessionCmd()` in `Register` (one line).

Not mine / left untouched:
- `internal/profile/model.go` shows a pre-existing unstaged gofmt-cosmetic diff
  from an earlier formatting pass (confirmed by Main). Not staged, not edited.

## Contract mapping
- Service methods correspond to routes without HTTP binding:
  `CreateSession` → `POST /api/v1/sessions`; `AppendTurn` →
  `POST /api/v1/sessions/{id}/turns`; `GetSession` → `GET /api/v1/sessions/{id}`;
  `GetExposure` → `GET /api/v1/sessions/{id}/exposure`.
- CLI `session start|turn|show|resume` calls the same service. `resume` emits the
  exact pinned snapshot canonical bytes (never re-resolves current state).
- Authenticated principal/owner/surface are injected by the adapter and derived
  server-side via `operations.Store.LocalAttribution(surface)`. Request DTOs carry
  no authoritative actor/principal/owner/delegated fields; `DecodeCreateSessionRequest`
  and `DecodeAppendTurnRequest` reject smuggled actor fields via
  `DisallowUnknownFields`.
- Session creation validates and pins the exact B0.2 canonical bytes through
  `profile.OpenSnapshot` and `agent.OpenExposureManifest`, cross-checking
  `exposure.SnapshotID() == snapshot.ID()`, and stores the canonical bytes; IDs are
  re-derived from those exact bytes on read.
- Every session/turn mutation and its outbox append commit in one
  `store.Update(ctx, func(*operations.Tx) error)` transaction; every event append
  uses `tx.AppendEvent`. Rollback emits no event (verified).
- Optimistic `ExpectedVersion` (SQL `UPDATE ... WHERE version=?`, plus an
  up-front check) prevents lost updates; `UNIQUE(session_id, idempotency_key)` plus
  an idempotent-replay short-circuit prevents duplicate turns.
- Inputs and bodies are bounded and UTF-8 validated; metadata/usage are
  allowlisted small JSON. Exposure output carries authority hashes and secret
  *references* only - source bodies and secret values never appear (verified).

## Schema / DB reuse
No second SQLite database was created. The service reuses the B0.1 `Store` with
`Update`/`View` and `tx.AppendEvent`, and writes only to the tables already
defined in `internal/operations/migrations/001_principal_sessions_outbox.sql`
(`sessions`, `session_turns`, `session_entries`, `outbox`). Reads inside a write
transaction use a shared `queryer` interface satisfied by both `*operations.Tx`
and `*operations.ReadTx`.

## Fixture use
`internal/session/testdata/session_v1.json` is hand-authored (not emitted by
production code). `TestSessionDocumentFixtureRoundTrip` decodes it with
`DisallowUnknownFields` (so any struct/wire drift fails), asserts hand-authored
values (version 2, one succeeded turn at ordinal 1 with resulting version 2, three
ordered entries message/tool_call/tool_result, `search_context` tool metadata),
then re-marshals and re-decodes and asserts a `reflect.DeepEqual` round trip. The
binary snapshot/exposure bytes are `json:"-"` (emitted raw by resume/exposure), so
the fixture stays a clean, hand-authorable ledger document.

## TDD evidence

### RED
Service (stub `service.go` returning `ErrInvalidRequest`):
```
--- FAIL: TestCreateTurnResume (0.00s)
    service_test.go:84: invalid session request
--- FAIL: TestRestart (0.00s)
    service_test.go:181: invalid session request
--- FAIL: TestExposure (0.00s)
    service_test.go:238: invalid session request
--- FAIL: TestActor (0.00s)
    service_test.go:313: invalid session request
FAIL	github.com/prowl-agent/prowl-agent/internal/session
```
(repository_test.go and the fixture round-trip test compiled and passed at this
step; only the service behavior was red.)

CLI (stub `session.go` returning `errSessionStub`):
```
--- FAIL: TestSessionCLIStartTurnShowResume (0.00s)
    session_test.go:89: start: session command not implemented ...
FAIL	github.com/prowl-agent/prowl-agent/internal/cli
```

### GREEN
Required selector:
```
$ go test -race -tags sqlite_fts5 ./internal/session -run 'Test(CreateTurnResume|Restart|Exposure|Actor)' -count=1
ok  	github.com/prowl-agent/prowl-agent/internal/session	1.038s
```
(verbose: TestCreateTurnResume, TestRestart, TestExposure, TestActor all PASS.)

Full session race suite:
```
$ go test -race -tags sqlite_fts5 ./internal/session -count=1
ok  	github.com/prowl-agent/prowl-agent/internal/session	1.041s
```

Full CLI suite (registration did not break existing commands):
```
$ go test -tags sqlite_fts5 ./internal/cli -count=1
ok  	github.com/prowl-agent/prowl-agent/internal/cli	4.509s
```

Vet:
```
$ go vet -tags sqlite_fts5 ./internal/session ./internal/cli
VET_EXIT=0
```

`gofmt -l` on all created/modified files: clean (no output).

## Behavioral coverage
- `TestCreateTurnResume`: create (version 1, active, pinned ids/bytes), append a
  message/tool_call/tool_result turn (ordinal 1, resulting version 2, ordered
  entries, terminal completion time, usage), idempotent replay returns the same
  turn without advancing state, stale `ExpectedVersion` with a fresh key →
  `ErrVersionConflict`, resume returns byte-identical pinned snapshot that reopens
  to the pinned id, missing session → `ErrSessionNotFound`.
- `TestRestart`: two turns across a close/reopen preserve version, order, run
  identity, non-terminal turn state, attribution, and frozen snapshot + exposure
  bytes.
- `TestExposure`: `GetExposure` returns the exact pinned bytes, reopens, exposes
  the `OPENAI_API_KEY` secret *reference*, leaks no source bodies, is byte-stable
  across reads, and returns `ErrSessionNotFound` for a missing session.
- `TestActor`: strict decoders reject smuggled `owner_principal_id`/`principal_id`/
  `surface_id`; a legitimate turn body decodes; persisted session and turn
  attribution equal the server-derived local principal, `cli` surface, and `local`
  scope/profile.
- Repository: rollback leaves no session row and no outbox event; optimistic
  version bump applies only when the stored version matches; duplicate idempotency
  key is rejected; entries preserve insertion order.
- CLI: `start --json` → `turn --json` → `show --json` → `resume` round trip against
  a real temp operations DB, and `show` without `--session` errors.

## Self-review
- One operations DB, one transaction per mutation, events via `tx.AppendEvent`
  only - atomicity and "rollback emits no event" are enforced and tested.
- Server-derived attribution is the only source of principal/owner/surface; the
  request DTOs are structurally free of actor fields and the JSON boundary rejects
  smuggling attempts.
- Pinned bytes are validated on write and reopened (never re-resolved) on read;
  resume/exposure return the stored canonical bytes verbatim.
- Bounds mirror the schema CHECK limits; entries/metadata/usage are UTF-8 and size
  bounded before any transaction begins.

## Concerns / notes for later tasks
- `GetSession`/`GetExposure` re-open the stored snapshot and exposure bytes on each
  read to derive their ids (no id columns exist in the B0.1 schema). This is a
  cheap validating parse for B0-scale sessions; if it ever becomes hot, B0.6+ could
  cache ids or add columns. Kept out of scope here to avoid a schema change B0.1
  owns.
- The CLI `session start` takes `--snapshot`/`--exposure` canonical-JSON files
  because the provider that composes snapshots server-side lands in B0.4; the
  service already validates and pins whatever bytes it is given, so B0.4/B0.6c can
  swap the source without touching the service contract.
- The outbox event metadata is intentionally the allowlisted lifecycle summary
  (`state`, `message_count`, `tool_call_count`); trajectory content lives only in
  the durable `session_entries` ledger, never in operations events.
- Did not commit; Main handles formatting and commit.
