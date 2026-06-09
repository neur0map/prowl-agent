package store

import (
	"strings"
	"testing"
)

func TestVectorStore(t *testing.T) {
	s := openTmp(t)
	fid, err := s.UpsertFile(File{RelPath: "a.css", Lang: "css", Hash: "h", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFileGraph(fid, nil, nil, nil, []Chunk{
		{StartLine: 1, EndLine: 1, Text: "alpha"},
		{StartLine: 2, EndLine: 2, Text: "beta"},
		{StartLine: 3, EndLine: 3, Text: "gamma"},
	}); err != nil {
		t.Fatal(err)
	}

	// Before vectors exist, every chunk needs embedding.
	cw, err := s.ChunksWithoutVectors()
	if err != nil || len(cw) != 3 {
		t.Fatalf("ChunksWithoutVectors=%d err=%v want 3", len(cw), err)
	}

	if err := s.EnableVectors(3, "test"); err != nil {
		t.Fatal(err)
	}
	if !s.VectorsReady() {
		t.Fatal("VectorsReady false after EnableVectors")
	}
	vecs := map[string][]float32{
		"alpha": {1, 0, 0},
		"beta":  {0, 1, 0},
		"gamma": {0, 0, 1},
	}
	for _, c := range cw {
		if err := s.UpsertChunkVector(c.ID, vecs[c.Text]); err != nil {
			t.Fatal(err)
		}
	}

	// KNN: query close to beta.
	hits, err := s.VectorSearch([]float32{0, 0.9, 0.1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || !strings.Contains(hits[0].Snippet, "beta") {
		t.Fatalf("VectorSearch nearest = %+v, want beta first", hits)
	}

	// All chunks now have vectors.
	if cw2, _ := s.ChunksWithoutVectors(); len(cw2) != 0 {
		t.Fatalf("ChunksWithoutVectors after embed = %d, want 0", len(cw2))
	}

	// Deleting the file clears its vectors.
	if err := s.DeleteFileByPath("a.css"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.VectorSearch([]float32{0, 1, 0}, 2); len(hits) != 0 {
		t.Fatalf("vectors not cleared after delete: %+v", hits)
	}
}
