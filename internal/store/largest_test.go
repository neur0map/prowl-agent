package store

import (
	"path/filepath"
	"testing"
)

func TestLargestFunctions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fid, err := s.UpsertFile(File{RelPath: "a.go", Lang: "go", Size: 100, Hash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	syms := []Symbol{
		{Name: "big", Kind: "function", StartLine: 10, EndLine: 60},    // 51 lines
		{Name: "small", Kind: "function", StartLine: 1, EndLine: 5},    // 5 lines
		{Name: "method", Kind: "method", StartLine: 70, EndLine: 100},  // 31 lines
		{Name: "oneLiner", Kind: "function", StartLine: 3, EndLine: 3}, // excluded (single line)
		{Name: "TypeX", Kind: "type", StartLine: 1, EndLine: 40},       // excluded (not func/method)
	}
	if err := s.ReplaceFileGraph(fid, syms, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	got, err := s.LargestFunctions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d functions, want 3 (single-line and non-func excluded): %+v", len(got), got)
	}
	// Ordered by span descending: big (51), method (31), small (5).
	if got[0].Name != "big" || got[0].Lines != 51 {
		t.Errorf("first = %+v, want big/51", got[0])
	}
	if got[1].Name != "method" || got[1].Kind != "method" {
		t.Errorf("second = %+v, want method", got[1])
	}
	if got[2].Name != "small" {
		t.Errorf("third = %+v, want small", got[2])
	}
}
