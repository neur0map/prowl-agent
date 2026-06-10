package cli

import (
	"context"
	"sync"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/index"
)

// freshness keeps the index current for MCP clients without a separate daemon or
// command. A featherweight fsnotify watcher only flags that something changed;
// the actual (incremental) re-index runs lazily, right before the next request
// that needs it, so an agent never reads stale data. The watcher stays active for
// idleWindow after the last request, then suspends to free file descriptors, and
// resumes on the next request (re-indexing once to catch up on anything missed).
type freshness struct {
	parent   context.Context
	root     string
	debounce time.Duration
	idle     time.Duration
	reindex  func(context.Context) (string, error)

	mu     sync.Mutex
	last   time.Time
	dirty  bool
	cancel context.CancelFunc // non-nil while the watcher is running
}

func newFreshness(parent context.Context, root string, reindex func(context.Context) (string, error)) *freshness {
	return &freshness{
		parent:   parent,
		root:     root,
		debounce: 750 * time.Millisecond,
		idle:     30 * time.Minute,
		reindex:  reindex,
	}
}

// start brings the watcher up at serve startup. The index is already fresh from
// the startup re-index, so this does not re-index.
func (f *freshness) start() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = time.Now()
	f.startWatcherLocked()
}

// onCall runs before each MCP request: it keeps the active window alive, re-indexes
// when the watcher flagged a change, and resumes (re-indexing to catch up) when the
// watcher had suspended after an idle period.
func (f *freshness) onCall() {
	f.mu.Lock()
	f.last = time.Now()
	if f.cancel == nil { // resumed from idle
		f.dirty = false
		f.startWatcherLocked()
		f.mu.Unlock()
		_, _ = f.reindex(f.parent)
		return
	}
	dirty := f.dirty
	f.dirty = false
	f.mu.Unlock()
	if dirty {
		_, _ = f.reindex(f.parent)
	}
}

// startWatcherLocked launches the watcher and idle monitor. The caller holds f.mu.
func (f *freshness) startWatcherLocked() {
	ctx, cancel := context.WithCancel(f.parent)
	f.cancel = cancel
	go func() {
		_ = index.Watch(ctx, f.root, f.debounce, func() {
			f.mu.Lock()
			f.dirty = true
			f.mu.Unlock()
		})
	}()
	go f.idleLoop(ctx)
}

// idleLoop suspends the watcher once no request has arrived for f.idle.
func (f *freshness) idleLoop(ctx context.Context) {
	step := f.idle / 4
	if step <= 0 {
		step = time.Second
	}
	t := time.NewTicker(step)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.mu.Lock()
			if f.cancel != nil && time.Since(f.last) > f.idle {
				f.cancel()
				f.cancel = nil
				f.mu.Unlock()
				return
			}
			f.mu.Unlock()
		}
	}
}
