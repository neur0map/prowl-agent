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
	cw, err := s.ChunksWithoutVectors(0)
	if err != nil || len(cw) != 3 {
		t.Fatalf("ChunksWithoutVectors=%d err=%v want 3", len(cw), err)
	}

	if err := s.EnableVectors(3, "test"); err != nil {
		t.Fatal(err)
	}
	if s.VectorsReady() {
		t.Fatal("VectorsReady true before generation publication")
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
	if err := s.SetMeta("vectors_complete", "1"); err != nil {
		t.Fatal(err)
	}
	if !s.VectorsReady() {
		t.Fatal("VectorsReady false after generation publication")
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
	if cw2, _ := s.ChunksWithoutVectors(0); len(cw2) != 0 {
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

func TestResetDerivedInvalidatesPublishedGeneration(t *testing.T) {
	s := openTmp(t)
	if err := s.SetMeta("index_state", "complete"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("vectors_complete", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableVectors(3, "test"); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetDerived(); err != nil {
		t.Fatal(err)
	}
	state, err := s.GetMeta("index_state")
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := s.GetMeta("vectors_complete")
	if err != nil {
		t.Fatal(err)
	}
	if state != "incomplete" || vectors != "0" || s.VectorsReady() {
		t.Fatalf("reset state=%q vectors_complete=%q ready=%v", state, vectors, s.VectorsReady())
	}
}

// Content-free chunks must never be embedded, and vectors an older version stored
// for them must be prunable. A whitespace-only chunk embeds to the model's
// degenerate mean vector, which ranks nearer an arbitrary query than real code
// does, so a few of them dominate every KNN result.
func TestContentFreeChunksAreNotEmbeddable(t *testing.T) {
	s := openTmp(t)
	fid, err := s.UpsertFile(File{RelPath: "a.go", Lang: "go", Hash: "h", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFileGraph(fid, nil, nil, nil, []Chunk{
		{StartLine: 1, EndLine: 1, Text: "func real() {}"},
		{StartLine: 2, EndLine: 2, Text: "\n"},
		{StartLine: 3, EndLine: 3, Text: "   \t  "},
	}); err != nil {
		t.Fatal(err)
	}

	pending, err := s.ChunksWithoutVectors(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Text != "func real() {}" {
		t.Fatalf("pending = %+v, want only the chunk with content", pending)
	}
	if n, err := s.CountEmbeddableChunks(); err != nil || n != 1 {
		t.Fatalf("CountEmbeddableChunks=%d err=%v want 1", n, err)
	}
	if n, err := s.CountChunksWithoutVectors(); err != nil || n != 1 {
		t.Fatalf("CountChunksWithoutVectors=%d err=%v want 1", n, err)
	}

	// Simulate an index built before the fix: a vector stored for a blank chunk.
	if err := s.EnableVectors(2, "test"); err != nil {
		t.Fatal(err)
	}
	blankID := int64(0)
	if err := s.sql().QueryRow(`SELECT id FROM chunks WHERE start_line=2`).Scan(&blankID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertChunkVector(blankID, []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	pruned, err := s.PruneContentFreeVectors()
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Fatalf("pruned %d, want 1", pruned)
	}
	if hits, _ := s.VectorSearch([]float32{1, 0}, 5); len(hits) != 0 {
		t.Fatalf("blank-chunk vector survived pruning: %+v", hits)
	}
}
