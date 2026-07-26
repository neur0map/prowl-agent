# Task B0.1 report -- durable principal and operations v1 store

## Status

Complete. The implementation is limited to `internal/operations/**` and creates
the single global operations SQLite authority required by later B0 tasks. It
does not add profile, provider, session service, route, UI, FTS, repair, or
management behavior.

Schema identity is exactly `prowl.operations/v1`. The default database path is
`$XDG_DATA_HOME/prowl-agent/operations.db` (or the standard user data fallback),
with directory mode 0700, database mode 0600, WAL, foreign keys, a 5-second busy
timeout, immediate write transactions, and idempotent close.

## RED evidence

### Migration and authority store

Tests and hand-authored v0/v1 fixtures were created before production code.

```text
go test -tags sqlite_fts5 ./internal/operations -run TestMigrationV1 -count=1
FAIL (build): DBPath, Open, SchemaIdentity, and ErrClosed were undefined
```

This was the expected failure for the absent operations store.

### Principal, attribution, and outbox

Principal/concurrency/client-attribution/transaction/cursor tests were then
created before principal and transaction production code.

```text
go test -tags sqlite_fts5 ./internal/operations \
  -run 'Test(Principal|ClientActorRejected)' -count=1
FAIL (build): ResolveLocalPrincipal, Tx, EventInput, Replay, watermark, and
retention APIs were undefined
```

This was the expected failure for the absent durable authority behavior.

## GREEN evidence

```text
go test -race -tags sqlite_fts5 ./internal/operations -run TestMigrationV1 -count=1
go test: 1 package ok

go test -race -tags sqlite_fts5 ./internal/operations \
  -run 'Test(MigrationV1|Principal|ClientActorRejected)' -count=1
go test: 1 package ok

go test -race -tags sqlite_fts5 ./internal/operations -count=1
go test: 1 package ok

go vet -tags sqlite_fts5 ./internal/operations
no output; exit 0
```

The full race package was rerun after formatting and after expanding migration
rollback/foreign-identity and session-plus-outbox atomicity coverage.

## Schema and migration evidence

`migrations/001_principal_sessions_outbox.sql` creates one transactional v1
schema containing:

- `operations_schema` with identity/version;
- one server-generated durable local operator in `principals`, with a partial
  unique index preventing a duplicate local operator;
- session-ready `sessions`, `session_turns`, and `session_entries` rows with
  separate principal, requested/resolved profile, surface, optional delegated
  and parent chain, owner, and authorization-scope dimensions;
- immutable operations outbox rows with scoped sequence, event ID/timestamp,
  schema/resource versions, event kind, server-derived attribution,
  correlations, and metadata bounded to 4096 bytes;
- one durable `operations` authority row with local-principal scope, epoch,
  retention floor, snapshot URI, and publisher watermark.

Migration tests prove fresh creation, v0 upgrade, v1 reopen, idempotent reopen,
future-version refusal without mutation, foreign-identity refusal, and rollback
of a deliberately incompatible partial migration. The v0/v1 fixtures are
literal SQL and do not use production code to generate expectations.

## Principal and transaction evidence

`ResolveLocalPrincipal` uses the immediate SQLite transaction boundary to
create at most one random local-operator ID and atomically bind the operations
cursor scope. Reopen returns the same ID/timestamp. Two independent Store
instances resolving concurrently return the same principal and leave one row.

Persistence constraints reject `source='client'`, unknown client actor IDs fail
foreign-key authority writes, and profile IDs remain separate from principals.
Only trusted internal transaction callbacks can write session authority rows;
later request DTOs must remain actorless as required by B0.3.

A session row and matching outbox event commit or roll back in one Store update.
Replay is scoped to the durable local principal; publisher watermark and
retention floor survive reopen; stale cursors return `ErrCursorExpired`.
Oversize/non-object metadata is rejected and an SQLite trigger rejects event
mutation.

## Changed paths

- `internal/operations/store.go`
- `internal/operations/store_test.go`
- `internal/operations/principal.go`
- `internal/operations/principal_test.go`
- `internal/operations/migrations/001_principal_sessions_outbox.sql`
- `internal/operations/testdata/v0.sql`
- `internal/operations/testdata/v1.sql`
- `.superpowers/sdd/2026-07-23-hermes-inspired-agent-operations-port/task-B0.1-report.md`

## Self-review and concerns

- The exported `Tx` is deliberately a short-lived trusted internal seam so B0.3
  can commit session state and outbox events in one database transaction without
  opening a second SQLite authority. It is invalid outside its callback.
- Client actor rejection at this layer is defense in depth through server-only
  principal sources, uniqueness, and foreign keys. B0.3 still owns actorless
  service/CLI request DTOs and explicit unknown-field rejection.
- Backups, broad repair, event publication, retention sweeps, session CRUD, and
  runtime lifecycle are intentionally deferred to their named later tasks.
- No project-wide suite was run for this scoped task.
