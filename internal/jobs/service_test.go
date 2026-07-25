package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type testPublisher struct{ calls atomic.Int32 }

func (p *testPublisher) PublishCommitted(context.Context) error { p.calls.Add(1); return nil }
func (p *testPublisher) Close() error                           { return nil }

func TestJobServiceCancelsRunningJob(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publisher := &testPublisher{}
	started := make(chan struct{})
	service := NewService(store, publisher, func(ctx context.Context, _ Job, progress func(string, int) error) error {
		close(started)
		if err := progress("indexing", 50); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	})
	job, _, err := service.EnqueueOrResumeIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}
	job, err = service.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, err = service.Cancel(context.Background(), job.ID, job.Version)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusCancelling {
		t.Fatalf("cancel status=%s", job.Status)
	}
	deadline := time.Now().Add(time.Second)
	for {
		job, err = service.Get(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == StatusCancelled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not cancel: %+v", job)
		}
		time.Sleep(time.Millisecond)
	}
	if job.Progress != 50 || publisher.calls.Load() == 0 {
		t.Fatalf("job=%+v publish=%d", job, publisher.calls.Load())
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
}
