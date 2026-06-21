package store

import (
	"path/filepath"
	"testing"
)

func TestStatsBump(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.BumpStats(1, 100, 1000); err != nil {
		t.Fatal(err)
	}
	if err := s.BumpStats(2, 50, 500); err != nil {
		t.Fatal(err)
	}
	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Queries != 3 || st.AnswerBytes != 150 || st.BaselineBytes != 1500 {
		t.Fatalf("stats = %+v, want {3 150 1500}", st)
	}
}

func TestFileSizes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.UpsertFile(File{RelPath: "a.lua", Lang: "lua", Size: 42, Hash: "h"}); err != nil {
		t.Fatal(err)
	}
	m, err := s.FileSizes()
	if err != nil {
		t.Fatal(err)
	}
	if m["a.lua"] != 42 {
		t.Fatalf("FileSizes = %v, want a.lua=42", m)
	}
}

func TestRecordAnswer(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.UpsertFile(File{RelPath: "bar/battery.lua", Lang: "lua", Size: 5000, Hash: "h1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertFile(File{RelPath: "bar/clock.lua", Lang: "lua", Size: 3000, Hash: "h2"}); err != nil {
		t.Fatal(err)
	}
	// An answer that references one indexed file and one unknown string.
	out := []SymbolHit{
		{ID: 1, Name: "battery", Kind: "widget", File: "bar/battery.lua", Line: 12},
		{ID: 2, Name: "elsewhere", Kind: "fn", File: "not/indexed.lua", Line: 1},
	}
	if err := s.RecordAnswer(out); err != nil {
		t.Fatal(err)
	}
	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Queries != 1 {
		t.Errorf("Queries = %d, want 1", st.Queries)
	}
	// Baseline counts only the indexed file's size; the unknown path is ignored.
	if st.BaselineBytes != 5000 {
		t.Errorf("BaselineBytes = %d, want 5000 (only the indexed file)", st.BaselineBytes)
	}
	if st.AnswerBytes <= 0 {
		t.Errorf("AnswerBytes = %d, want > 0", st.AnswerBytes)
	}
}
