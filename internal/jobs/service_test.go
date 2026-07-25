package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type testPublisher struct{ calls atomic.Int32 }

func (p *testPublisher) PublishCommitted(context.Context) error { p.calls.Add(1); return nil }
func (p *testPublisher) Close() error                           { return nil }

type blockingPublisher struct {
	calls   atomic.Int32
	blockAt int32
	entered chan struct{}
	release chan struct{}
}

func (p *blockingPublisher) PublishCommitted(context.Context) error {
	if p.calls.Add(1) == p.blockAt {
		close(p.entered)
		<-p.release
	}
	return nil
}

func (p *blockingPublisher) Close() error { return nil }

func waitForTerminalJob(t *testing.T, service *Service, id string) Job {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		job, err := service.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Terminal() {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not become terminal: %+v", job)
		}
		time.Sleep(time.Millisecond)
	}
}

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

func TestJobServiceReconcilesCancellationBeforeStartWithoutRunning(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var runs atomic.Int32
	service := NewService(store, &testPublisher{}, func(context.Context, Job, func(string, int) error) error {
		runs.Add(1)
		return nil
	})
	first, _, err := service.EnqueueOrResumeIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Cancel(context.Background(), first.ID, first.Version); err != nil {
		t.Fatal(err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancelled := waitForTerminalJob(t, service, first.ID)
	if cancelled.Status != StatusCancelled || cancelled.Outcome != "cancelled" || runs.Load() != 0 {
		t.Fatalf("cancelled=%+v runs=%d", cancelled, runs.Load())
	}
	second, created, err := service.EnqueueOrResumeIndex(context.Background())
	if err != nil || !created || second.ID == first.ID {
		t.Fatalf("second=%+v created=%v err=%v", second, created, err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJobServiceConsumesQueuedCancellationWhileWorkerIsLive(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var block atomic.Bool
	claimEntered := make(chan struct{}, 1)
	releaseClaim := make(chan struct{})
	var runs atomic.Int32
	service := NewService(store, &testPublisher{}, func(context.Context, Job, func(string, int) error) error {
		runs.Add(1)
		return nil
	})
	service.beforeClaim = func() {
		if block.Load() {
			select {
			case claimEntered <- struct{}{}:
			default:
			}
			<-releaseClaim
		}
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	block.Store(true)
	queued, _, err := service.EnqueueOrResumeIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-claimEntered:
	case <-time.After(time.Second):
		t.Fatal("worker did not pause before claim")
	}
	if _, err := service.Cancel(context.Background(), queued.ID, queued.Version); err != nil {
		t.Fatal(err)
	}
	block.Store(false)
	close(releaseClaim)
	cancelled := waitForTerminalJob(t, service, queued.ID)
	if cancelled.Status != StatusCancelled || runs.Load() != 0 {
		t.Fatalf("cancelled=%+v runs=%d", cancelled, runs.Load())
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJobServiceCancelsClaimedJobBeforeRunnerRegistration(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publisher := &blockingPublisher{blockAt: 2, entered: make(chan struct{}), release: make(chan struct{})}
	var runs atomic.Int32
	service := NewService(store, publisher, func(context.Context, Job, func(string, int) error) error {
		runs.Add(1)
		return nil
	})
	job, _, err := service.EnqueueOrResumeIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-publisher.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not publish claimed job")
	}
	claimed, err := service.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Cancel(context.Background(), claimed.ID, claimed.Version); err != nil {
		t.Fatal(err)
	}
	close(publisher.release)
	cancelled := waitForTerminalJob(t, service, job.ID)
	if cancelled.Status != StatusCancelled || runs.Load() != 0 {
		t.Fatalf("cancelled=%+v runs=%d", cancelled, runs.Load())
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerActivePreventsLiveJobReconciliationAcrossStores(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()
	store1, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	service1 := NewService(store1, &testPublisher{}, func(ctx context.Context, _ Job, _ func(string, int) error) error {
		<-ctx.Done()
		return ctx.Err()
	})
	job, _, err := service1.EnqueueOrResumeIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	running := make(chan struct{})
	service1.runner = func(ctx context.Context, _ Job, _ func(string, int) error) error {
		close(running)
		<-ctx.Done()
		return ctx.Err()
	}
	if err := service1.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-running:
	case <-time.After(time.Second):
		t.Fatal("first worker did not claim job")
	}

	store2, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var secondRuns atomic.Int32
	service2 := NewService(store2, &testPublisher{}, func(context.Context, Job, func(string, int) error) error {
		secondRuns.Add(1)
		return nil
	})
	if err := service2.Start(context.Background()); !errors.Is(err, ErrWorkerActive) {
		t.Fatalf("second start error=%v, want ErrWorkerActive", err)
	}
	live, err := store2.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if live.Status != StatusRunning || secondRuns.Load() != 0 {
		t.Fatalf("live=%+v second runs=%d", live, secondRuns.Load())
	}
	if err := service2.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service1.Close(); err != nil {
		t.Fatal(err)
	}

	store3, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var resumedRuns atomic.Int32
	service3 := NewService(store3, &testPublisher{}, func(context.Context, Job, func(string, int) error) error {
		resumedRuns.Add(1)
		return nil
	})
	if err := service3.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	resumed := waitForTerminalJob(t, service3, job.ID)
	if resumed.Status != StatusSucceeded || resumedRuns.Load() != 1 {
		t.Fatalf("resumed=%+v runs=%d", resumed, resumedRuns.Load())
	}
	if err := service3.Close(); err != nil {
		t.Fatal(err)
	}
}
