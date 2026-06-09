package index

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch watches root recursively (skipping .prowl/.git and other tool dirs) and
// calls onChange, debounced by the given interval, whenever a file changes. It
// blocks until ctx is cancelled. onChange runs on its own goroutine, so callers
// must make it concurrency-safe.
func Watch(ctx context.Context, root string, debounce time.Duration, onChange func()) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	addTree(w, root)

	var timer *time.Timer
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if skipPath(ev.Name) {
				continue
			}
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					addTree(w, ev.Name)
				}
			}
			if timer == nil {
				timer = time.AfterFunc(debounce, onChange)
			} else {
				timer.Reset(debounce)
			}
		case _, ok := <-w.Errors:
			if !ok {
				return nil
			}
		}
	}
}

func addTree(w *fsnotify.Watcher, root string) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if alwaysSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			_ = w.Add(p)
		}
		return nil
	})
}

func skipPath(p string) bool {
	for skip := range alwaysSkipDirs {
		if strings.Contains(p, string(filepath.Separator)+skip+string(filepath.Separator)) ||
			strings.HasSuffix(p, string(filepath.Separator)+skip) {
			return true
		}
	}
	return false
}
