package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureAgentsBlockRefreshesInPlace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	// A user's own notes, with a stale prowl block sandwiched between them.
	stale := agentsMarker + "\nold guidance: use prowl-agent serve only\n" + agentsEndMarker
	original := "# My project\n\nSome house rules.\n\n" + stale + "\n\n## Other section\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureAgentsBlock(path); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)

	// Stale block content is replaced with the current guidance.
	if strings.Contains(s, "old guidance: use prowl-agent serve only") {
		t.Errorf("stale block content not replaced:\n%s", s)
	}
	if !strings.Contains(s, "Query the index from your shell") {
		t.Errorf("current guidance missing:\n%s", s)
	}
	// The user's surrounding content is preserved.
	if !strings.Contains(s, "Some house rules.") || !strings.Contains(s, "## Other section") {
		t.Errorf("user content not preserved:\n%s", s)
	}
	// Exactly one block (no duplication).
	if n := strings.Count(s, agentsMarker); n != 1 {
		t.Errorf("expected exactly one block marker, got %d:\n%s", n, s)
	}
	if n := strings.Count(s, agentsEndMarker); n != 1 {
		t.Errorf("expected exactly one end marker, got %d:\n%s", n, s)
	}
}

func TestEnsureAgentsBlockAppendsWhenAbsent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# Existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureAgentsBlock(path); err != nil {
		t.Fatal(err)
	}
	s, _ := os.ReadFile(path)
	if !strings.Contains(string(s), "# Existing") || !strings.Contains(string(s), agentsMarker) {
		t.Errorf("expected existing content plus appended block:\n%s", s)
	}
}
