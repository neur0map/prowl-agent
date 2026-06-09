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
