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

func TestSearchChunksPhraseToTermsFallback(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mk := func(rel, text string) {
		fid, err := s.UpsertFile(File{RelPath: rel, Lang: "go", Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceFileGraph(fid, nil, nil, nil, []Chunk{{StartLine: 1, EndLine: 1, Text: text}}); err != nil {
			t.Fatal(err)
		}
	}
	mk("a.go", "draw the status panel and render it inline")
	mk("b.go", "an unrelated gamma helper")

	// Exact phrase present -> the phrase tier matches a.go.
	if hits, err := s.SearchChunks("status panel", 10); err != nil || len(hits) != 1 || hits[0].File != "a.go" {
		t.Fatalf("phrase search = %v, %v; want 1 hit in a.go", hits, err)
	}
	// Phrase absent but all terms co-occur -> the AND tier matches a.go.
	if hits, err := s.SearchChunks("render status panel", 10); err != nil || len(hits) != 1 || hits[0].File != "a.go" {
		t.Fatalf("AND fallback = %v, %v; want 1 hit in a.go", hits, err)
	}
	// No chunk has both terms -> the OR tier returns each chunk matching either.
	if hits, err := s.SearchChunks("panel gamma", 10); err != nil || len(hits) != 2 {
		t.Fatalf("OR fallback = %v, %v; want both files", hits, err)
	}
	// A genuinely absent token yields empty with no error.
	if hits, err := s.SearchChunks("nonexistenttoken", 10); err != nil || len(hits) != 0 {
		t.Fatalf("absent term = %v, %v; want empty", hits, err)
	}
}
