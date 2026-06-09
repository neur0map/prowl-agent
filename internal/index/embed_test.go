package index

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// fakeEmbedder is a deterministic stand-in for an embedding model: it maps text
// to a fixed-dim vector by byte folding. It exercises the real storage and
// retrieval paths without needing a live Ollama.
type fakeEmbedder struct{ dim int }

func (f fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, f.dim)
		for j, b := range []byte(t) {
			v[j%f.dim] += float32(b)
		}
		out[i] = v
	}
	return out, nil
}

func (f fakeEmbedder) Generate(_ context.Context, _ string) (string, error) { return "", nil }

func TestBuildVectors(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root := filepath.Join("..", "..", "testdata", "rice-hypr")
	if _, err := Index(s, root, nil); err != nil {
		t.Fatal(err)
	}

	pending, _ := s.ChunksWithoutVectors()
	if len(pending) == 0 {
		t.Fatal("no chunks to embed")
	}

	n, err := BuildVectors(context.Background(), s, fakeEmbedder{dim: 32}, "fake")
	if err != nil {
		t.Fatal(err)
	}
	if n != len(pending) {
		t.Fatalf("embedded %d, want %d", n, len(pending))
	}
	if !s.VectorsReady() {
		t.Fatal("vectors not ready after build")
	}
	if left, _ := s.ChunksWithoutVectors(); len(left) != 0 {
		t.Fatalf("%d chunks still without vectors", len(left))
	}

	// Incremental: a second run embeds nothing.
	if n2, err := BuildVectors(context.Background(), s, fakeEmbedder{dim: 32}, "fake"); err != nil || n2 != 0 {
		t.Fatalf("re-run embedded %d err=%v, want 0", n2, err)
	}

	// A vector search returns results.
	hits, err := s.VectorSearch(make([]float32, 32), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("vector search returned nothing")
	}
}
