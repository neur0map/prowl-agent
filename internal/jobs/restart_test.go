package jobs

import (
	"context"
	"testing"
)

func TestRestartReconcilesRunningIndexJobOnce(t *testing.T) {
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
	running, claimed, err := store.claim(context.Background())
	if err != nil || !claimed {
		t.Fatalf("claim=%+v claimed=%v err=%v", running, claimed, err)
	}
	if err := store.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	resumed, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != StatusQueued || resumed.ID != job.ID || resumed.Version <= running.Version {
		t.Fatalf("reconciled=%+v", resumed)
	}
	duplicate, created, err := store.EnqueueOrResumeIndex(context.Background())
	if err != nil || created || duplicate.ID != job.ID {
		t.Fatalf("duplicate=%+v created=%v err=%v", duplicate, created, err)
	}
}

func TestRestartReconcilesRunningExportJobOnce(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, _, err := store.EnqueueOrResumeExport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	running, claimed, err := store.claimRunnable(context.Background(), true)
	if err != nil || !claimed || running.ID != job.ID || running.Kind != KindExport {
		t.Fatalf("claim=%+v claimed=%v err=%v", running, claimed, err)
	}
	if err := store.reconcileRunnable(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	resumed, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != StatusQueued || resumed.ID != job.ID || resumed.Version <= running.Version {
		t.Fatalf("reconciled=%+v", resumed)
	}
	duplicate, created, err := store.EnqueueOrResumeExport(context.Background())
	if err != nil || created || duplicate.ID != job.ID {
		t.Fatalf("duplicate=%+v created=%v err=%v", duplicate, created, err)
	}
}
