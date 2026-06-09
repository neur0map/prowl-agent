package assist

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaClient(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[]}`))
	})
	mux.HandleFunc("/api/embed", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{0.1, 0.2}, {0.3, 0.4}}})
	})
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"response": "ok"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	o := NewOllama(srv.URL, "embed", "gen")
	ctx := context.Background()

	if !o.Available(ctx) {
		t.Fatal("expected Available true")
	}
	emb, err := o.Embed(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(emb) != 2 || len(emb[0]) != 2 || emb[1][1] != 0.4 {
		t.Fatalf("embeddings = %v", emb)
	}
	gen, err := o.Generate(ctx, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if gen != "ok" {
		t.Fatalf("generate = %q", gen)
	}

	// A closed server is not available.
	srv.Close()
	if o.Available(ctx) {
		t.Fatal("expected Available false after shutdown")
	}
}
