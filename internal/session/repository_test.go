package session

import (
	"context"
	"errors"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/operations"
)

func openTestStore(t *testing.T) *operations.Store {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := operations.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func testSessionRow(attribution operations.Attribution, id string) sessionRow {
	return sessionRow{
		id:                 id,
		version:            1,
		status:             string(SessionActive),
		principalID:        attribution.PrincipalID(),
		requestedProfileID: attribution.RequestedProfileID(),
		resolvedProfileID:  attribution.ResolvedProfileID(),
		surfaceID:          string(attribution.SurfaceID()),
		ownerPrincipalID:   attribution.OwnerPrincipalID(),
		authorizationScope: attribution.AuthorizationScope(),
		snapshotJSON:       []byte("{}"),
		exposureJSON:       []byte("{}"),
		createdAt:          1,
		updatedAt:          1,
	}
}

// TestRepositoryRollbackEmitsNoEvent proves state and outbox commit atomically:
// a failed transaction leaves neither a session row nor an operations event.
func TestRepositoryRollbackEmitsNoEvent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	var repo repository
	attribution, err := store.LocalAttribution(ctx, operations.SurfaceCLI)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("rollback")
	err = store.Update(ctx, func(tx *operations.Tx) error {
		if err := repo.insertSession(ctx, tx, testSessionRow(attribution, "rollback-session")); err != nil {
			return err
		}
		if _, err := tx.AppendEvent(ctx, attribution, operations.EventInput{ResourceKind: "session", ResourceID: "rollback-session", ResourceVersion: 1, Kind: "session.created", Metadata: operations.EventMetadata{State: operations.EventStateActive}}); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("rollback error=%v", err)
	}
	state, err := store.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Head != 0 {
		t.Fatalf("rollback published an event: head=%d", state.Head)
	}
	if err := store.View(ctx, func(rtx *operations.ReadTx) error {
		if _, found, err := repo.session(ctx, rtx, "rollback-session"); err != nil {
			return err
		} else if found {
			t.Fatal("rollback retained a session row")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryOptimisticVersionAndIdempotency(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	var repo repository
	attribution, err := store.LocalAttribution(ctx, operations.SurfaceCLI)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, func(tx *operations.Tx) error {
		return repo.insertSession(ctx, tx, testSessionRow(attribution, "s1"))
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.Update(ctx, func(tx *operations.Tx) error {
		applied, err := repo.bumpSessionVersion(ctx, tx, "s1", 1, 2, 2)
		if err != nil {
			return err
		}
		if !applied {
			t.Fatal("fresh version bump not applied")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, func(tx *operations.Tx) error {
		applied, err := repo.bumpSessionVersion(ctx, tx, "s1", 1, 2, 3)
		if err != nil {
			return err
		}
		if applied {
			t.Fatal("stale version bump was applied")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	turn := turnRow{
		id: "t1", sessionID: "s1", ordinal: 1, idempotencyKey: "key",
		expectedVersion: 1, resultingVersion: 2, runID: "r", status: string(TurnSucceeded),
		principalID: attribution.PrincipalID(), surfaceID: string(attribution.SurfaceID()),
		ownerPrincipalID: attribution.OwnerPrincipalID(), authorizationScope: attribution.AuthorizationScope(),
		usageJSON: []byte("{}"), createdAt: 1,
	}
	if err := store.Update(ctx, func(tx *operations.Tx) error {
		return repo.insertTurn(ctx, tx, turn)
	}); err != nil {
		t.Fatal(err)
	}
	duplicate := turn
	duplicate.id = "t2"
	duplicate.ordinal = 2
	if err := store.Update(ctx, func(tx *operations.Tx) error {
		return repo.insertTurn(ctx, tx, duplicate)
	}); err == nil {
		t.Fatal("duplicate idempotency key accepted")
	}

	if err := store.View(ctx, func(rtx *operations.ReadTx) error {
		next, err := repo.nextTurnOrdinal(ctx, rtx, "s1")
		if err != nil {
			return err
		}
		if next != 2 {
			t.Fatalf("next turn ordinal=%d", next)
		}
		got, found, err := repo.turnByIdempotency(ctx, rtx, "s1", "key")
		if err != nil {
			return err
		}
		if !found || got.id != "t1" || got.resultingVersion != 2 {
			t.Fatalf("idempotent turn lookup=%+v found=%v", got, found)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryEntriesPreserveOrder(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	var repo repository
	attribution, err := store.LocalAttribution(ctx, operations.SurfaceCLI)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, func(tx *operations.Tx) error {
		if err := repo.insertSession(ctx, tx, testSessionRow(attribution, "s1")); err != nil {
			return err
		}
		if err := repo.insertTurn(ctx, tx, turnRow{
			id: "t1", sessionID: "s1", ordinal: 1, idempotencyKey: "key", expectedVersion: 1,
			resultingVersion: 2, runID: "r", status: string(TurnSucceeded), principalID: attribution.PrincipalID(),
			surfaceID: string(attribution.SurfaceID()), ownerPrincipalID: attribution.OwnerPrincipalID(),
			authorizationScope: attribution.AuthorizationScope(), usageJSON: []byte("{}"), createdAt: 1,
		}); err != nil {
			return err
		}
		for index, body := range []string{"first", "second", "third"} {
			if err := repo.insertEntry(ctx, tx, entryRow{
				id: "e" + body, sessionID: "s1", turnID: "t1", ordinal: int64(index + 1),
				kind: string(EntryMessage), body: []byte(body), metadata: []byte("{}"),
				principalID: attribution.PrincipalID(), surfaceID: string(attribution.SurfaceID()),
				ownerPrincipalID: attribution.OwnerPrincipalID(), authorizationScope: attribution.AuthorizationScope(),
				createdAt: int64(index + 1),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.View(ctx, func(rtx *operations.ReadTx) error {
		entries, err := repo.entries(ctx, rtx, "s1")
		if err != nil {
			return err
		}
		if len(entries) != 3 {
			t.Fatalf("entries=%d", len(entries))
		}
		if string(entries[0].body) != "first" || string(entries[1].body) != "second" || string(entries[2].body) != "third" {
			t.Fatalf("entry order not preserved: %+v", entries)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
