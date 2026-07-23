package cli

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestFreshness(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	var n int32
	reindex := func(context.Context) (string, error) { atomic.AddInt32(&n, 1); return "ok", nil }
	f := newFreshness(parent, t.TempDir(), reindex)
	f.idle = 60 * time.Millisecond

	f.start()
	defer f.stop()
	if got := atomic.LoadInt32(&n); got != 0 {
		t.Fatalf("start should not re-index, got %d", got)
	}

	// Active window, nothing changed: no re-index.
	_ = f.onCall(context.Background())
	if got := atomic.LoadInt32(&n); got != 0 {
		t.Fatalf("clean onCall should not re-index, got %d", got)
	}

	// A change the watcher flagged: the next call re-indexes once.
	f.mu.Lock()
	f.dirty = true
	f.mu.Unlock()
	_ = f.onCall(context.Background())
	if got := atomic.LoadInt32(&n); got != 1 {
		t.Fatalf("dirty onCall should re-index once, got %d", got)
	}

	// After the idle window the watcher suspends.
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		suspended := f.cancel == nil
		f.mu.Unlock()
		if suspended {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watcher did not suspend after the idle window")
		}
		time.Sleep(15 * time.Millisecond)
	}

	// The next call resumes and re-indexes to catch up, watcher running again.
	_ = f.onCall(context.Background())
	if got := atomic.LoadInt32(&n); got != 2 {
		t.Fatalf("resume should re-index to catch up, got %d", got)
	}
	f.mu.Lock()
	running := f.cancel != nil
	f.mu.Unlock()
	if !running {
		t.Fatal("watcher should be running again after resume")
	}
}

func TestFreshnessRetainsDirtyStateAfterRefreshFailure(t *testing.T) {
	sentinel := errors.New("refresh failed")
	var calls atomic.Int32
	f := newFreshness(context.Background(), t.TempDir(), func(context.Context) (string, error) {
		if calls.Add(1) == 1 {
			return "", sentinel
		}
		return "ok", nil
	})
	f.start()
	defer f.stop()
	f.mu.Lock()
	f.dirty = true
	f.mu.Unlock()
	if err := f.onCall(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("first call error = %v, want sentinel", err)
	}
	f.mu.Lock()
	dirty := f.dirty
	f.mu.Unlock()
	if !dirty {
		t.Fatal("failed refresh cleared dirty state")
	}
	if err := f.onCall(context.Background()); err != nil {
		t.Fatalf("retry refresh: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("refresh calls = %d, want 2", calls.Load())
	}
}

func TestFreshnessWatcherFailureForcesCatchUp(t *testing.T) {
	var watchCalls atomic.Int32
	var reindexCalls atomic.Int32
	f := newFreshness(context.Background(), t.TempDir(), func(context.Context) (string, error) {
		reindexCalls.Add(1)
		return "ok", nil
	})
	f.watch = func(ctx context.Context, _ string, _ time.Duration, _ func()) error {
		if watchCalls.Add(1) == 1 {
			return errors.New("inotify exhausted")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	f.start()
	deadline := time.Now().Add(time.Second)
	for {
		f.mu.Lock()
		failed := f.cancel == nil && f.dirty
		f.mu.Unlock()
		if failed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watcher failure was not recorded")
		}
		time.Sleep(time.Millisecond)
	}
	if err := f.onCall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reindexCalls.Load() != 1 {
		t.Fatalf("reindex calls = %d, want 1", reindexCalls.Load())
	}
	f.stop()
}

func TestFreshnessCallGateHonorsCancellation(t *testing.T) {
	f := newFreshness(context.Background(), t.TempDir(), func(context.Context) (string, error) { return "", nil })
	f.callGate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.onCall(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("onCall blocked gate error = %v, want canceled", err)
	}
	<-f.callGate
}
