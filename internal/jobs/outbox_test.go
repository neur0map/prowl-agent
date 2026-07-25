package jobs

import (
	"context"
	"testing"
)

func TestOutboxTransactionCreatesChangedEvent(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, _, err := store.EnqueueOrResumeIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rows, more, err := store.Replay(context.Background(), 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if more || len(rows) != 1 || rows[0].Kind != "project-job.changed" || job.ID == "" {
		t.Fatalf("rows=%+v more=%v job=%+v", rows, more, job)
	}
	if err := store.AdvanceRetention(context.Background(), rows[0].Sequence, "snapshot://jobs"); err != nil {
		t.Fatal(err)
	}
	state, err := store.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.RetentionFloor != rows[0].Sequence || state.SnapshotURI != "snapshot://jobs" {
		t.Fatalf("state=%+v", state)
	}
}

func TestAdvanceRetentionRejectsFloorPastHead(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, _, err := store.EnqueueOrResumeIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AdvanceRetention(context.Background(), state.Head+1, "snapshot://jobs"); err == nil {
		t.Fatal("AdvanceRetention accepted floor beyond head")
	}
	got, err := store.Get(context.Background(), job.ID)
	if err != nil || got.ID != job.ID {
		t.Fatalf("job=%+v err=%v", got, err)
	}
}
