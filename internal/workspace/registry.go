package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Entry records one initialized project in the global registry.
type Entry struct {
	Root      string `json:"root"`
	CreatedAt int64  `json:"created_at"`
	AI        bool   `json:"ai"`
}

// registryPath returns $XDG_STATE_HOME/prowl-agent/registry.json (or the
// ~/.local/state fallback).
func registryPath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "prowl-agent", "registry.json"), nil
}

// Register upserts a project (by absolute root) into the global registry.
func Register(root string, ai bool) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	p, err := registryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	entries, _ := List()
	found := false
	for i := range entries {
		if entries[i].Root == abs {
			entries[i].AI = ai
			found = true
		}
	}
	if !found {
		entries = append(entries, Entry{Root: abs, CreatedAt: time.Now().Unix(), AI: ai})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Root < entries[j].Root })
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// List returns all registered projects.
func List() ([]Entry, error) {
	p, err := registryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
