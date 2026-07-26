package operations

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestPrincipalGeneratedDurableAcrossReopen(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	principal, err := store.ResolveLocalPrincipal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !validID(principal.ID) || principal.Kind != PrincipalLocalOperator || principal.Source != PrincipalSourceServerLocal || principal.CreatedAt.IsZero() {
		t.Fatalf("principal=%+v", principal)
	}
	state, err := store.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.StreamScope != "operations" || state.ScopeID != principal.ID || state.SnapshotURI != "snapshot://operations/"+principal.ID {
		t.Fatalf("state=%+v principal=%+v", state, principal)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again, err := reopened.ResolveLocalPrincipal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != principal.ID || !again.CreatedAt.Equal(principal.CreatedAt) {
		t.Fatalf("first=%+v reopened=%+v", principal, again)
	}
	var count int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM principals WHERE kind='local-operator'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("local principal count=%d", count)
	}
}

func TestPrincipalConcurrentResolutionCannotDuplicate(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	first, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	start := make(chan struct{})
	results := make(chan Principal, 2)
	errorsCh := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	resolve := func(store *Store) {
		ready.Done()
		<-start
		principal, err := store.ResolveLocalPrincipal(context.Background())
		results <- principal
		errorsCh <- err
	}
	go resolve(first)
	go resolve(second)
	ready.Wait()
	close(start)
	left, right := <-results, <-results
	leftErr, rightErr := <-errorsCh, <-errorsCh
	if leftErr != nil || rightErr != nil {
		t.Fatalf("left=%+v err=%v right=%+v err=%v", left, leftErr, right, rightErr)
	}
	if left.ID == "" || left.ID != right.ID {
		t.Fatalf("left=%+v right=%+v", left, right)
	}
	var count int
	if err := first.db.QueryRow(`SELECT COUNT(*) FROM principals WHERE kind='local-operator'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("local principal count=%d", count)
	}
}

func TestClientActorRejectedByAuthorityConstraints(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	principal, err := store.ResolveLocalPrincipal(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	err = store.Update(context.Background(), func(tx *Tx) error {
		_, err := tx.ExecContext(context.Background(), `INSERT INTO principals(principal_id,kind,source,parent_principal_id,created_at) VALUES(?,?,?,?,?)`, "client-actor", "delegated", "client", nil, 1)
		return err
	})
	if err == nil {
		t.Fatal("client-sourced principal was accepted")
	}

	err = store.Update(context.Background(), func(tx *Tx) error {
		_, err := tx.ExecContext(context.Background(), `INSERT INTO sessions(session_id,version,status,principal_id,requested_profile_id,resolved_profile_id,surface_id,delegated_principal_id,parent_session_id,owner_principal_id,authorization_scope,snapshot_json,exposure_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			"session-client", 1, "active", "client-actor", "local", "local", "workbench", nil, nil, "client-actor", "local", []byte(`{}`), []byte(`{}`), 1, 1)
		return err
	})
	if err == nil {
		t.Fatal("client actor was accepted as session authority")
	}

	err = store.Update(context.Background(), func(tx *Tx) error {
		_, err := tx.ExecContext(context.Background(), `INSERT INTO sessions(session_id,version,status,principal_id,requested_profile_id,resolved_profile_id,surface_id,delegated_principal_id,parent_session_id,owner_principal_id,authorization_scope,snapshot_json,exposure_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			"session-local", 1, "active", principal.ID, "profile-requested", "profile-resolved", "workbench", nil, nil, principal.ID, "local", []byte(`{}`), []byte(`{}`), 1, 1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var principalCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM principals`).Scan(&principalCount); err != nil {
		t.Fatal(err)
	}
	if principalCount != 1 {
		t.Fatalf("profile identities created principals: count=%d", principalCount)
	}
}

func TestPrincipalOutboxTransactionAndCursorAreDurable(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	principal, err := store.ResolveLocalPrincipal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	input := EventInput{
		ResourceKind:       "session",
		ResourceID:         "session-1",
		ResourceVersion:    1,
		Kind:               "session.created",
		PrincipalID:        principal.ID,
		RequestedProfileID: "local",
		ResolvedProfileID:  "local",
		SurfaceID:          "cli",
		OwnerPrincipalID:   principal.ID,
		AuthorizationScope: "local",
		CorrelationID:      "correlation-1",
		Metadata:           []byte(`{"status":"active"}`),
	}
	injected := errors.New("rollback")
	err = store.Update(context.Background(), func(tx *Tx) error {
		if err := insertTestSession(context.Background(), tx, principal, "session-1"); err != nil {
			return err
		}
		if _, err := tx.AppendEvent(context.Background(), input); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("rollback error=%v", err)
	}
	state, err := store.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Head != 0 {
		t.Fatalf("rollback published outbox head=%d", state.Head)
	}
	var sessionCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if sessionCount != 0 {
		t.Fatalf("rollback retained sessions=%d", sessionCount)
	}

	var event Event
	if err := store.Update(context.Background(), func(tx *Tx) error {
		if err := insertTestSession(context.Background(), tx, principal, "session-1"); err != nil {
			return err
		}
		created, err := tx.AppendEvent(context.Background(), input)
		event = created
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if sessionCount != 1 {
		t.Fatalf("committed sessions=%d", sessionCount)
	}
	if event.Sequence != 1 || !validID(event.ID) || event.SchemaVersion != EventSchemaVersion || event.OccurredAt.IsZero() {
		t.Fatalf("event=%+v", event)
	}
	rows, more, err := store.Replay(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if more || len(rows) != 1 || rows[0].ID != event.ID || string(rows[0].Metadata) != `{"status":"active"}` {
		t.Fatalf("rows=%+v more=%v", rows, more)
	}
	if err := store.SetPublisherWatermark(context.Background(), event.Sequence); err != nil {
		t.Fatal(err)
	}
	if err := store.AdvanceRetention(context.Background(), event.Sequence, "snapshot://operations/"+principal.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Replay(context.Background(), 0, 10); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expired replay error=%v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	state, err = reopened.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.ScopeID != principal.ID || state.Head != 1 || state.RetentionFloor != 1 || state.PublisherWatermark != 1 {
		t.Fatalf("reopened state=%+v", state)
	}
}

func TestPrincipalOutboxRejectsUnboundedMetadataAndMutation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	principal, err := store.ResolveLocalPrincipal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	input := EventInput{ResourceKind: "session", ResourceID: "session-1", ResourceVersion: 1, Kind: "session.created", PrincipalID: principal.ID, RequestedProfileID: "local", ResolvedProfileID: "local", SurfaceID: "cli", OwnerPrincipalID: principal.ID, AuthorizationScope: "local", Metadata: []byte(`{}`)}
	input.Metadata = []byte(`{"value":"` + strings.Repeat("x", MaxEventMetadataBytes) + `"}`)
	if err := store.Update(context.Background(), func(tx *Tx) error {
		_, err := tx.AppendEvent(context.Background(), input)
		return err
	}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("oversize metadata error=%v", err)
	}
	input.Metadata = []byte(`{}`)
	var event Event
	if err := store.Update(context.Background(), func(tx *Tx) error {
		created, err := tx.AppendEvent(context.Background(), input)
		event = created
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), func(tx *Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE outbox SET event_kind='tampered' WHERE event_id=?`, event.ID)
		return err
	}); err == nil {
		t.Fatal("immutable outbox event was updated")
	}
}

func insertTestSession(ctx context.Context, tx *Tx, principal Principal, id string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO sessions(session_id,version,status,principal_id,requested_profile_id,resolved_profile_id,surface_id,delegated_principal_id,parent_session_id,owner_principal_id,authorization_scope,snapshot_json,exposure_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, 1, "active", principal.ID, "local", "local", "cli", nil, nil, principal.ID, "local", []byte(`{}`), []byte(`{}`), 1, 1)
	return err
}
