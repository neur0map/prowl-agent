package store

import "testing"

// docCommentHits returns the rel_paths whose symbol doc comments match the FTS
// query, reading fts_docs directly. There is deliberately no symbol-shaped
// production search over it: the two real consumers read differently
// (ChunksByDocMatch joins fts_docs to chunks; SymbolDocsInFile reads the column),
// so this coverage queries the table itself to prove the triggers keep it in sync.
func docCommentHits(t *testing.T, s *Store, match string) []string {
	t.Helper()
	rows, err := s.sql().Query(`SELECT f.rel_path FROM fts_docs
		JOIN symbols sy ON sy.id = fts_docs.rowid JOIN files f ON f.id = sy.file_id
		WHERE fts_docs MATCH ? ORDER BY bm25(fts_docs)`, match)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// The doc comment is indexed in its own fts_docs field, so prose that answers a
// query but is buried in a code-dominated chunk is still searchable -- the
// docstring answering "keep secrets out of the log file" was absent from all 50
// results before this field existed. porter stemming makes the query's "logs"
// match the docstring's "logs"/"log". The trigger migration is load-bearing: an
// index that kept its old docs-unaware trigger would leave fts_docs empty.
func TestDocFieldIndexedInFtsDocs(t *testing.T) {
	s := openTmp(t)
	fid, err := s.UpsertFile(File{RelPath: "redact.py", Lang: "python", Hash: "h", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	// The query words appear only in the doc, not in the code chunk.
	if err := s.ReplaceFileGraph(fid, []Symbol{{
		Name: "redact_text", Kind: "function", StartLine: 10, EndLine: 20,
		Doc: "Regex-based secret redaction for logs and tool output.",
	}}, nil, nil, []Chunk{{StartLine: 10, EndLine: 20, Text: "def redact_text(s):\n    return _PATTERNS.sub(_mask, s)"}}); err != nil {
		t.Fatal(err)
	}
	if hits := docCommentHits(t, s, "secret AND logs"); len(hits) == 0 || hits[0] != "redact.py" {
		t.Fatalf("fts_docs match = %v, want redact.py first", hits)
	}
}

// Replacing a file's graph keeps symbols.doc and fts_docs in step: the old doc's
// entry is removed (symbols_ad fires on the delete) and the new one indexed, and
// deleting the file clears it. A stale entry would surface a doc the source no
// longer contains -- the reindex-goes-stale failure.
func TestDocFollowsReplaceAndDelete(t *testing.T) {
	s := openTmp(t)
	fid, err := s.UpsertFile(File{RelPath: "svc.go", Lang: "go", Hash: "h", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	docColumn := func() string {
		var d string
		if err := s.sql().QueryRow(`SELECT IFNULL(doc,'') FROM symbols WHERE file_id=?`, fid).Scan(&d); err != nil {
			t.Fatal(err)
		}
		return d
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
	if got := docColumn(); got != "alphaunique brings the listener online" {
		t.Fatalf("doc column after first write = %q", got)
	}
	if h := docCommentHits(t, s, "alphaunique"); len(h) != 1 {
		t.Fatalf("fts_docs(alphaunique) = %v, want 1", h)
	}

	put("betaunique takes the listener offline")
	if got := docColumn(); got != "betaunique takes the listener offline" {
		t.Fatalf("stored doc went stale after replace: %q", got)
	}
	if h := docCommentHits(t, s, "alphaunique"); len(h) != 0 {
		t.Fatalf("stale fts_docs entry survived replace: %v", h)
	}
	if h := docCommentHits(t, s, "betaunique"); len(h) != 1 {
		t.Fatalf("replaced doc not indexed: %v", h)
	}

	if err := s.DeleteFileByPath("svc.go"); err != nil {
		t.Fatal(err)
	}
	if h := docCommentHits(t, s, "betaunique"); len(h) != 0 {
		t.Fatalf("fts_docs not cleaned after delete: %v", h)
	}
}
