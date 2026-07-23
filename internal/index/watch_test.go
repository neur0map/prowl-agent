package index

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.lua"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fired := make(chan struct{}, 8)
	go func() {
		_ = Watch(ctx, root, 40*time.Millisecond, func() {
			select {
			case fired <- struct{}{}:
			default:
			}
		})
	}()
	time.Sleep(200 * time.Millisecond) // let inotify watches register

	if err := os.WriteFile(filepath.Join(root, "a.lua"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not fire on file change")
	}

	// A file created in a newly-created subdirectory is also detected.
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(sub, "b.lua"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not fire on new-subdir file")
	}
}

func TestWatchCancellationJoinsActiveCallback(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, root, 10*time.Millisecond, func() {
			close(started)
			<-release
		})
	}()
	time.Sleep(200 * time.Millisecond)
	if err := os.WriteFile(path, []byte("package b"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("watch callback did not start")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Watch returned before callback completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Watch error = %v, want canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not return after callback completed")
	}
}

func TestWatchRejectsMissingRoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := Watch(ctx, filepath.Join(t.TempDir(), "missing"), time.Millisecond, func() {})
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Watch missing root error = %v, want immediate filesystem error", err)
	}
}
