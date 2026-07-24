// Package workspace manages a project's .prowl/ directory, the global registry
// of initialized projects, and gitignore wiring.
package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// Dir is the per-project workspace directory name.
const Dir = ".prowl"

// ErrNotFound is returned when no .prowl workspace is found.
var ErrNotFound = errors.New("no .prowl workspace found (run 'prowl-agent init')")

// Workspace locates a project's index and config.
type Workspace struct {
	Root      string // project root containing .prowl/
	Path      string // path to .prowl/
	DB        string // derived SQLite index
	Knowledge string // canonical, trackable OKF bundle
	Proposals string // reviewable, optionally trackable proposal inbox
	Cache     string // derived cache data
	Logs      string // derived logs
}

func at(root string) *Workspace {
	d := filepath.Join(root, Dir)
	return &Workspace{
		Root: root, Path: d,
		DB:        filepath.Join(d, "index.db"),
		Knowledge: filepath.Join(d, "knowledge"),
		Proposals: filepath.Join(d, "proposals"),
		Cache:     filepath.Join(d, "cache"),
		Logs:      filepath.Join(d, "logs"),
	}
}

// Create makes the .prowl/ workspace (and logs dir) under root.
func Create(root string) (*Workspace, error) {
	w := at(root)
	if err := os.MkdirAll(filepath.Join(w.Path, "logs"), 0o755); err != nil {
		return nil, err
	}
	return w, nil
}

// ResolveContext bounds workspace discovery even when an underlying filesystem
// metadata operation cannot itself observe context cancellation. Resolve retains
// its synchronous compatibility behavior.
func ResolveContext(ctx context.Context, start string) (*Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		workspace *Workspace
		err       error
	}
	resolved := make(chan result, 1)
	go func() {
		workspace, err := Resolve(start)
		resolved <- result{workspace: workspace, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resolved:
		return result.workspace, result.err
	}
}

// Resolve walks up from start to find an existing .prowl/ workspace.
func Resolve(start string) (*Workspace, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for {
		cand := filepath.Join(dir, Dir)
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return at(dir), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotFound
		}
		dir = parent
	}
}
