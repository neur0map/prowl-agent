package store

import (
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/redact"
)

// ChunksContainingMarker is the durable, at-rest evidence that secrets were
// masked: per file, how many stored chunks carry the marker, ordered by path.
func TestChunksContainingMarker(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	put := func(rel string, chunks ...Chunk) {
		id, err := s.UpsertFile(File{RelPath: rel, Lang: "python", Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceFileGraph(id, nil, nil, nil, chunks); err != nil {
			t.Fatal(err)
		}
	}
	masked := `TOKEN = "` + redact.Mask + `"`
	put("a.py", Chunk{StartLine: 1, EndLine: 1, Text: masked}, Chunk{StartLine: 3, EndLine: 3, Text: masked})
	put("b.py", Chunk{StartLine: 1, EndLine: 1, Text: masked}, Chunk{StartLine: 5, EndLine: 5, Text: "clean = 1"})
	put("c.py", Chunk{StartLine: 1, EndLine: 1, Text: "no secrets here"})

	got, err := s.ChunksContainingMarker(redact.Mask)
	if err != nil {
		t.Fatal(err)
	}
	want := []MarkerCount{{File: "a.py", Count: 2}, {File: "b.py", Count: 1}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %+v, want %+v (full %+v)", i, got[i], want[i], got)
		}
	}
}

// The secret value itself is never stored, so a query for it at rest returns
// nothing while the marker that replaced it is present. This is the direct
// at-rest counterpart to the index test's search-path check.
func TestChunksContainingMarkerSecretAbsentAtRest(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.UpsertFile(File{RelPath: "config.py", Lang: "python", Hash: "h", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFileGraph(id, nil, nil, nil, []Chunk{
		{StartLine: 1, EndLine: 1, Text: `STRIPE_TOKEN = "` + redact.Mask + `"`},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := s.ChunksContainingMarker(redact.Mask)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].File != "config.py" || hits[0].Count != 1 {
		t.Fatalf("marker query = %+v, want config.py x1", hits)
	}
	leaked, err := s.ChunksContainingMarker("sk_live_" + "51H8xQ2eZvKYlo2CabcdefghijklmnopQRST")
	if err != nil {
		t.Fatal(err)
	}
	if len(leaked) != 0 {
		t.Fatalf("secret present in chunks.text at rest: %+v", leaked)
	}
}
