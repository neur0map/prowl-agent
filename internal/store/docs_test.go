package store

import "testing"

// The doc comment must be searchable on its own, because scoring it inside a
// 1,334-byte chunk buries it: the docstring that answers "keep secrets out of the
// log file" was absent from all 50 results before this field existed.
func TestDocFieldIsSearchableIndependentlyOfChunkText(t *testing.T) {
	s := openTmp(t)
	fid, err := s.UpsertFile(File{RelPath: "redact.py", Lang: "python", Hash: "h", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFileGraph(fid, []Symbol{{
		Name: "redact_text", Kind: "function", StartLine: 10, EndLine: 20,
		Doc: "Regex-based secret redaction for logs and tool output.",
	}}, nil, nil, []Chunk{{StartLine: 10, EndLine: 20, Text: "def redact_text(s):\n    return _PATTERNS.sub(_mask, s)"}}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchDocs("secret logs", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("doc field did not match; the words appear only in the doc, not the code")
	}
	if hits[0].Name != "redact_text" || hits[0].File != "redact.py" {
		t.Fatalf("hit = %+v, want redact_text in redact.py", hits[0])
	}
}

// Replacing a file's graph must keep fts_docs in step: the old doc's entry is
// removed (symbols_ad fires on the delete) and the new one indexed. A stale
// fts_docs entry would surface a doc the source no longer contains.
func TestDocFTSFollowsReplaceAndDelete(t *testing.T) {
	s := openTmp(t)
	fid, err := s.UpsertFile(File{RelPath: "svc.go", Lang: "go", Hash: "h", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	put := func(doc string) {
		t.Helper()
		if err := s.ReplaceFileGraph(fid,
			[]Symbol{{Name: "Serve", Kind: "function", StartLine: 1, EndLine: 2, Doc: doc}},
			nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	put("alphaunique brings the listener online")
	if hits, _ := s.SearchDocs("alphaunique", 10); len(hits) != 1 {
		t.Fatalf("SearchDocs(alphaunique) = %+v, want 1", hits)
	}

	put("betaunique takes the listener offline")
	if hits, _ := s.SearchDocs("alphaunique", 10); len(hits) != 0 {
		t.Fatalf("stale doc still indexed after replace: %+v", hits)
	}
	if hits, _ := s.SearchDocs("betaunique", 10); len(hits) != 1 {
		t.Fatalf("replaced doc not indexed: %+v", hits)
	}

	if err := s.DeleteFileByPath("svc.go"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.SearchDocs("betaunique", 10); len(hits) != 0 {
		t.Fatalf("fts_docs not cleaned after delete: %+v", hits)
	}
}
