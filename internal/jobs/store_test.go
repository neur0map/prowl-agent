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
}
