package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestReindexer(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "sample-config"))
	r := reindexer(s, root, nil, "", nil)

	msg, err := r(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "indexed=11") {
		t.Fatalf("first pass = %q, want indexed=11", msg)
	}
	// A second pass should re-parse nothing.
	if msg2, _ := r(context.Background()); !strings.Contains(msg2, "skipped=11") {
		t.Fatalf("second pass = %q, want skipped=11", msg2)
	}
}

func TestMaybeInferencerDisabled(t *testing.T) {
	if inf := maybeInferencer(context.Background(), config.Config{}); inf != nil {
		t.Fatal("inferencer should be nil when AI is disabled")
	}
}
