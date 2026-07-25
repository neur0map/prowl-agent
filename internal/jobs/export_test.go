package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportJobArtifactIsPrivateAndReadableOnlyAfterSuccess(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var service *Service
	service = NewService(store, &testPublisher{}, func(context.Context, Job, func(string, int) error) error { return nil })
	if err := service.SetExportRunner(func(ctx context.Context, job Job, progress func(string, int) error) error {
		if err := progress("writing", 50); err != nil {
			return err
		}
		return service.WriteExportArtifact(ctx, job.ID, []byte("<!doctype html><title>offline</title>"))
	}); err != nil {
		t.Fatal(err)
	}
	job, _, err := service.EnqueueOrResumeExport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadExportArtifact(context.Background(), job.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("queued artifact error=%v want ErrInvalidTransition", err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed := waitForTerminalJob(t, service, job.ID)
	if completed.Status != StatusSucceeded || completed.Outcome != "exported" {
		t.Fatalf("completed=%+v", completed)
	}
	artifact, err := service.ReadExportArtifact(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(artifact), "<!doctype html><title>offline</title>"; got != want {
		t.Fatalf("artifact=%q want=%q", got, want)
	}
	directory := filepath.Join(filepath.Dir(store.Path()), "exports")
	if info, err := os.Stat(directory); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory info=%v err=%v", info, err)
	}
	if info, err := os.Stat(filepath.Join(directory, job.ID+".html")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("artifact info=%v err=%v", info, err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledExportJobDoesNotRetainArtifact(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var service *Service
	service = NewService(store, &testPublisher{}, func(context.Context, Job, func(string, int) error) error { return nil })
	if err := service.SetExportRunner(func(ctx context.Context, job Job, progress func(string, int) error) error {
		if err := service.WriteExportArtifact(ctx, job.ID, []byte("<!doctype html><title>cancelled</title>")); err != nil {
			return err
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	job, _, err := service.EnqueueOrResumeExport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("export runner did not persist its artifact")
	}
	live, err := service.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Cancel(context.Background(), job.ID, live.Version); err != nil {
		t.Fatal(err)
	}
	cancelled := waitForTerminalJob(t, service, job.ID)
	if cancelled.Status != StatusCancelled {
		t.Fatalf("cancelled=%+v", cancelled)
	}
	if _, err := service.ReadExportArtifact(context.Background(), job.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("cancelled artifact error=%v want ErrInvalidTransition", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(store.Path()), "exports", job.ID+".html")); !os.IsNotExist(err) {
		t.Fatalf("cancelled artifact remains: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
}
