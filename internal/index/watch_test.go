package index

import (
	"context"
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
