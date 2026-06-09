package store

import (
	"path/filepath"
	"testing"
)

func openTmp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestFilesCRUD(t *testing.T) {
	s := openTmp(t)
	id, err := s.UpsertFile(File{RelPath: "a.lua", Lang: "lua", Hash: "h1", Size: 10, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetFileByPath("a.lua")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.ID != id || got.Lang != "lua" || got.Hash != "h1" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	// Re-upsert with a new hash must update in place (same id).
	id2, err := s.UpsertFile(File{RelPath: "a.lua", Lang: "lua", Hash: "h2", Size: 11, MTime: 2})
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id {
		t.Fatalf("upsert created new id %d (want %d)", id2, id)
	}
	if g, _, _ := s.GetFileByPath("a.lua"); g.Hash != "h2" {
		t.Fatalf("hash not updated: %q", g.Hash)
	}
}

func TestReplaceFileGraphAndSearch(t *testing.T) {
	s := openTmp(t)
	fid, err := s.UpsertFile(File{RelPath: "init.lua", Lang: "lua", Hash: "h", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	err = s.ReplaceFileGraph(fid,
		[]Symbol{{Name: "M.setup", Kind: "function", StartLine: 3, EndLine: 9}},
		[]Resource{{Kind: "color", Name: "--accent", Value: "#1e1e2e", Line: 2}},
		[]RawEdge{{Kind: "includes", Raw: "foo.bar", Line: 1}},
		[]Chunk{{StartLine: 1, EndLine: 9, Text: "require foo.bar accent setup"}},
	)
	if err != nil {
		t.Fatal(err)
	}

	if hits, _ := s.SymbolsByName("M.setup", 10); len(hits) != 1 || hits[0].File != "init.lua" || hits[0].Line != 3 {
		t.Fatalf("SymbolsByName = %+v", hits)
	}
	if hits, _ := s.SearchSymbols("setup", 10); len(hits) != 1 {
		t.Fatalf("SearchSymbols(setup) = %+v", hits)
	}
	if hits, _ := s.SearchChunks("accent", 10); len(hits) != 1 || hits[0].File != "init.lua" {
		t.Fatalf("SearchChunks(accent) = %+v", hits)
	}

	// Replacing must not duplicate rows.
	if err := s.ReplaceFileGraph(fid, []Symbol{{Name: "M.setup", Kind: "function", StartLine: 3, EndLine: 9}}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.SymbolsByName("M.setup", 10); len(hits) != 1 {
		t.Fatalf("after replace, SymbolsByName = %d rows, want 1", len(hits))
	}

	// Deleting the file must clear symbols + FTS rows (cascade + triggers).
	if err := s.DeleteFileByPath("init.lua"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.SearchSymbols("setup", 10); len(hits) != 0 {
		t.Fatalf("FTS not cleaned after delete: %+v", hits)
	}
	if hits, _ := s.SearchChunks("accent", 10); len(hits) != 0 {
		t.Fatalf("chunk FTS not cleaned after delete: %+v", hits)
	}
}
