package cli

import (
	"context"
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
	if got := atomic.LoadInt32(&n); got != 0 {
		t.Fatalf("start should not re-index, got %d", got)
	}

	// Active window, nothing changed: no re-index.
	f.onCall()
	if got := atomic.LoadInt32(&n); got != 0 {
		t.Fatalf("clean onCall should not re-index, got %d", got)
	}

	// A change the watcher flagged: the next call re-indexes once.
	f.mu.Lock()
	f.dirty = true
	f.mu.Unlock()
	f.onCall()
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
	f.onCall()
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
