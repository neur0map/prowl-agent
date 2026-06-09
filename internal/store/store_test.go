package store

import (
	"path/filepath"
	"testing"
)

func TestOpenMigrate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "i.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.GetMeta("schema_version")
	if err != nil {
		t.Fatal(err)
	}
	if v != "1" {
		t.Fatalf("schema_version=%q want 1", v)
	}
	if err := s.SetMeta("x", "y"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.GetMeta("x"); v != "y" {
		t.Fatalf("meta x=%q want y", v)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Re-open must be idempotent.
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	s2.Close()
}
