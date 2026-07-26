package operations

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationV1CreatesPrivateOperationsAuthority(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	path, err := DBPath()
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(data, "prowl-agent", "operations.db")
	if path != wantPath {
		t.Fatalf("path=%q want=%q", path, wantPath)
	}

	store, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("directory permissions=%o want=700", info.Mode().Perm())
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("database permissions=%o", info.Mode().Perm())
	}

	var identity string
	var version int
	if err := store.db.QueryRow(`SELECT identity, version FROM operations_schema WHERE id=1`).Scan(&identity, &version); err != nil {
		t.Fatal(err)
	}
	if identity != SchemaIdentity || version != 1 {
		t.Fatalf("schema identity=%q version=%d", identity, version)
	}
	rows, err := store.db.Query(`SELECT name FROM sqlite_master WHERE name IN ('operations_schema','principals','sessions','session_turns','session_entries','outbox','authority','one_local_operator','outbox_immutable_update')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"operations_schema", "principals", "sessions", "session_turns", "session_entries", "outbox", "authority", "one_local_operator", "outbox_immutable_update"} {
		if !found[name] {
			t.Fatalf("migration did not create %q: %v", name, found)
		}
	}
	state, err := store.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.StreamScope != "operations" || state.ScopeID != "" || state.Epoch != 1 || state.Head != 0 || state.RetentionFloor != 0 || state.PublisherWatermark != 0 {
		t.Fatalf("state=%+v", state)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := store.State(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed state error=%v", err)
	}
}

func TestMigrationV1UpgradesAndReopensFixtures(t *testing.T) {
	for _, fixture := range []string{"v0.sql", "v1.sql"} {
		t.Run(fixture, func(t *testing.T) {
			data := t.TempDir()
			t.Setenv("XDG_DATA_HOME", data)
			path, err := DBPath()
			if err != nil {
				t.Fatal(err)
			}
			installOperationsFixture(t, path, fixture)
			store, err := Open(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			var identity string
			var version int
			if err := reopened.db.QueryRow(`SELECT identity, version FROM operations_schema WHERE id=1`).Scan(&identity, &version); err != nil {
				t.Fatal(err)
			}
			if identity != SchemaIdentity || version != 1 {
				t.Fatalf("schema identity=%q version=%d", identity, version)
			}
		})
	}
}

func TestMigrationV1RejectsFutureSchemaWithoutMutation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	path, err := DBPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE operations_schema (id INTEGER PRIMARY KEY, identity TEXT NOT NULL, version INTEGER NOT NULL); INSERT INTO operations_schema VALUES(1,'prowl.operations/v1',2)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background()); err == nil {
		t.Fatal("Open accepted future schema")
	}
	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, tables int
	if err := db.QueryRow(`SELECT version FROM operations_schema WHERE id=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if version != 2 || tables != 1 {
		t.Fatalf("future schema mutated: version=%d tables=%d", version, tables)
	}
}

func TestMigrationV1RejectsForeignIdentity(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	path, err := DBPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE operations_schema (id INTEGER PRIMARY KEY, identity TEXT NOT NULL, version INTEGER NOT NULL); INSERT INTO operations_schema VALUES(1,'foreign.operations/v1',1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background()); err == nil {
		t.Fatal("Open accepted foreign schema identity")
	}
}

func TestMigrationV1FailureRollsBack(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	path, err := DBPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE operations_schema (id INTEGER PRIMARY KEY CHECK (id=1), identity TEXT NOT NULL, version INTEGER NOT NULL); CREATE TABLE principals (broken TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background()); err == nil {
		t.Fatal("Open accepted incompatible partial schema")
	}
	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var schemaRows, createdTables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM operations_schema`).Scan(&schemaRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('sessions','session_turns','session_entries','outbox','authority')`).Scan(&createdTables); err != nil {
		t.Fatal(err)
	}
	if schemaRows != 0 || createdTables != 0 {
		t.Fatalf("failed migration was not rolled back: schema_rows=%d created_tables=%d", schemaRows, createdTables)
	}
}

func installOperationsFixture(t *testing.T, path, fixture string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(contents)); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
