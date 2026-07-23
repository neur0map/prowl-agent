package index

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch watches root recursively (skipping .prowl/.git and other tool dirs) and
// calls onChange, debounced by the given interval, whenever a file changes. It
// blocks until ctx is cancelled. The callback completes synchronously, so no
// callback can outlive Watch's return.
func Watch(ctx context.Context, root string, debounce time.Duration, onChange func()) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := addTree(ctx, w, root); err != nil {
		return err
	}

	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var timerC <-chan time.Time

	resetTimer := func() {
		if timerC != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(debounce)
		timerC = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timerC:
			timerC = nil
			onChange()
		case ev, ok := <-w.Events:
			if !ok {
				return errors.New("filesystem watcher event stream closed")
			}
			if skipPath(ev.Name) {
				continue
			}
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					if err := addTree(ctx, w, ev.Name); err != nil {
						return err
					}
				}
			}
			resetTimer()
		case watchErr, ok := <-w.Errors:
			if !ok {
				return errors.New("filesystem watcher error stream closed")
			}
			return fmt.Errorf("filesystem watcher: %w", watchErr)
		}
	}
}

func addTree(ctx context.Context, w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			if alwaysSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if err := w.Add(p); err != nil {
				return fmt.Errorf("watch %s: %w", p, err)
			}
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
