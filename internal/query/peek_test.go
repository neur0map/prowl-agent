package query

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPeekLines(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 500; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	// A normal inclusive range returns exactly those lines.
	pk, err := PeekLines(dir, "f.txt", 10, 12)
	if err != nil {
		t.Fatal(err)
	}
	if pk.StartLine != 10 || pk.EndLine != 12 || pk.Truncated {
		t.Fatalf("range: got %d-%d trunc=%v", pk.StartLine, pk.EndLine, pk.Truncated)
	}
	if pk.Text != "line 10\nline 11\nline 12" {
		t.Fatalf("text: %q", pk.Text)
	}

	// An end past EOF clamps to the last line, not an error.
	pk, err = PeekLines(dir, "f.txt", 499, 9999)
	if err != nil {
		t.Fatal(err)
	}
	if pk.EndLine != 500 {
		t.Fatalf("clamp end: got %d, want 500", pk.EndLine)
	}

	// A span wider than the cap is truncated to PeekMaxLines.
	pk, err = PeekLines(dir, "f.txt", 1, 500)
	if err != nil {
		t.Fatal(err)
	}
	if pk.EndLine != PeekMaxLines || !pk.Truncated {
		t.Fatalf("cap: got end=%d trunc=%v, want end=%d trunc=true", pk.EndLine, pk.Truncated, PeekMaxLines)
	}

	// Traversal outside the root is rejected.
	if _, err := PeekLines(dir, "../secret", 1, 1); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}
