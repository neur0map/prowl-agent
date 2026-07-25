package jobs

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobsDBPathUsesPrivateXDGProjectDirectory(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	root := filepath.Join(t.TempDir(), "workspace")
	path, id, err := DBPath(root)
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(data, "prowl-agent", "projects", id)
	if filepath.Dir(path) != wantDir || filepath.Base(path) != "jobs.db" {
		t.Fatalf("path=%q, id=%q", path, id)
	}
	if strings.Contains(path, ".prowl") || strings.HasSuffix(path, "index.db") {
		t.Fatalf("jobs path leaked index path: %q", path)
	}
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	info, err := os.Stat(wantDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("directory permissions=%o, want 700", info.Mode().Perm())
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("database permissions=%o", info.Mode().Perm())
	}
}

func TestJobEnqueueAndCancelAreDurable(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, created, err := store.EnqueueOrResumeIndex(context.Background())
	if err != nil || !created {
		t.Fatalf("enqueue job=%+v created=%v err=%v", job, created, err)
	}
	cancelled, err := store.Cancel(context.Background(), job.ID, job.Version)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != StatusCancelling {
		t.Fatalf("status=%s", cancelled.Status)
	}
}

func TestJobsMigrationUpgradesV0Artifact(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()
	path, _, err := DBPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	v0, err := os.ReadFile(filepath.Join("testdata", "v0.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(v0)); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.db.Query(`SELECT name FROM sqlite_master WHERE type IN ('table', 'index') AND name IN ('jobs_schema', 'jobs', 'outbox', 'authority', 'one_active_index_job')`)
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
	for _, name := range []string{"jobs_schema", "jobs", "outbox", "authority", "one_active_index_job"} {
		if !found[name] {
			t.Fatalf("migration did not create %q: %v", name, found)
		}
	}
	if _, created, err := store.EnqueueOrResumeIndex(context.Background()); err != nil || !created {
		t.Fatalf("enqueue after migration created=%v err=%v", created, err)
	}
	if _, err := store.State(context.Background()); err != nil {
		t.Fatalf("authority state after migration: %v", err)
	}
}

func TestJobsMigrationRefusesFutureSchema(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()
	path, _, err := DBPath(root)
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
	if _, err := db.Exec(`CREATE TABLE jobs_schema (identity TEXT NOT NULL, version INTEGER NOT NULL); INSERT INTO jobs_schema VALUES ('prowl.project-jobs/v1', 2)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := Open(context.Background(), root); err == nil {
		t.Fatal("Open accepted future schema")
	}
	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`SELECT version FROM jobs_schema`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("future schema version rewritten to %d", version)
	}
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 1 {
		t.Fatalf("future schema was migrated: tables=%d", tables)
	}
}

func TestConcurrentEnqueueReturnsOneDurableJobAcrossStores(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()
	first, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	type result struct {
		job     Job
		created bool
		err     error
	}
	enteredInsert := make(chan struct{}, 2)
	releaseInsert := make(chan struct{})
	beforeInsert := func() {
		enteredInsert <- struct{}{}
		<-releaseInsert
	}
	first.beforeEnqueueInsert = beforeInsert
	second.beforeEnqueueInsert = beforeInsert
	start := make(chan struct{})
	results := make(chan result, 2)
	enqueue := func(store *Store) {
		<-start
		job, created, err := store.EnqueueOrResumeIndex(context.Background())
		results <- result{job: job, created: created, err: err}
	}
	go enqueue(first)
	go enqueue(second)
	close(start)
	<-enteredInsert
	close(releaseInsert)
	left, right := <-results, <-results
	if left.err != nil || right.err != nil {
		t.Fatalf("left=%+v right=%+v", left, right)
	}
	if left.job.ID == "" || left.job.ID != right.job.ID {
		t.Fatalf("left=%+v right=%+v", left, right)
	}
	if (left.created && right.created) || (!left.created && !right.created) {
		t.Fatalf("created left=%v right=%v", left.created, right.created)
	}
	state, err := first.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Head != 1 {
		t.Fatalf("outbox head=%d, want 1", state.Head)
	}
}
