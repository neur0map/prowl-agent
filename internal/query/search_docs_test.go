package query

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// The regression this whole phase exists for: prose that answers the query lives
// in a doc comment inside a chunk dominated by code, and must still rank. On the
// real corpus, agent/redact.py -- whose docstring reads "secret redaction for
// logs ... before they reach log files" -- was absent from all 50 results for
// "keep secrets out of the log file", because BM25 averaged its short docstring
// against its long import block. Fusing a doc-comment signal into the ranked
// chunk list surfaces it without displacing the code hits that already answered.
func TestSearchSurfacesDocOnlyMatches(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// Ten decoys whose code mentions "log" but whose purpose is unrelated, plus one
	// file whose doc comment is the answer and whose code shares no query token.
	mkDocFixture(t, s)
	hits, err := New(s).SimilarCode(context.Background(), "keep secrets out of the log file")
	if err != nil {
		t.Fatal(err)
	}
	rank := -1
	for i, h := range hits {
		if h.File == "redact.py" {
			rank = i + 1
			break
		}
	}
	if rank == -1 || rank > 5 {
		t.Fatalf("redact.py rank = %d, want within the top 5; got %s", rank, fmtHits(hits))
	}
	// The doc signal is additive, not a filter: the ten decoys whose code lexically
	// answers the query must still appear, and no result may be blank (the prior
	// whitespace-chunk bug).
	var decoys int
	for _, h := range hits {
		if h.File == "" {
			t.Fatalf("blank result in %s", fmtHits(hits))
		}
		if h.File != "redact.py" {
			decoys++
		}
	}
	if decoys < 10 {
		t.Fatalf("doc signal displaced lexical hits: only %d decoys survived in %s", decoys, fmtHits(hits))
	}
}

// mkDocFixture inserts eleven files: ten decoys whose chunk text contains "log"
// (so lexical search ranks them) with docs unrelated to the query, and redact.py
// whose code shares no query token but whose doc comment is the answer.
func mkDocFixture(t *testing.T, s *store.Store) {
	t.Helper()
	put := func(rel, lang, code, doc string) {
		fid, err := s.UpsertFile(store.File{RelPath: rel, Lang: lang, Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		var syms []store.Symbol
		if doc != "" {
			syms = []store.Symbol{{Name: "sym", Kind: "function", StartLine: 1, EndLine: 2, Doc: doc}}
		}
		if err := s.ReplaceFileGraph(fid, syms, nil, nil, []store.Chunk{{StartLine: 1, EndLine: 2, Text: code}}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 10 {
		put(fmt.Sprintf("mod%d.go", i), "go",
			fmt.Sprintf("func handle%d() {\n    log.Printf(\"event %d handled\")\n}", i, i),
			"Handles inbound request routing for a subsystem.")
	}
	put("redact.py", "python",
		"def _sub(p, s):\n    return p.sub(_m, s)",
		"Regex-based secret redaction for logs and tool output. Masks API keys, tokens and credentials before they reach log files.")
}

func fmtHits(hits []store.ChunkHit) string {
	var b []string
	for i, h := range hits {
		b = append(b, fmt.Sprintf("%d:%s:%d", i+1, h.File, h.StartLine))
	}
	return fmt.Sprintf("%v", b)
}
