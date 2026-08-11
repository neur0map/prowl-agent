package assist

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFakeAgent writes an executable /bin/sh script that stands in for a
// coding-agent CLI, so the tests exercise the real subprocess path without a
// model.
func writeFakeAgent(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake agent uses /bin/sh")
	}
	path := filepath.Join(t.TempDir(), "fakeagent")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAgentCLIRerankParsesOrder(t *testing.T) {
	bin := writeFakeAgent(t, `echo "1, 0, 2"`)
	order, err := (&AgentCLI{Argv: []string{bin}}).Rerank(context.Background(), "q", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 0, 2}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestAgentCLIRerankAppendsMissingIndices(t *testing.T) {
	// A model that names only one index still yields a full permutation.
	bin := writeFakeAgent(t, `echo "2"`)
	order, err := (&AgentCLI{Argv: []string{bin}}).Rerank(context.Background(), "q", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[0] != 2 {
		t.Fatalf("order = %v, want a length-3 permutation starting with 2", order)
	}
}

func TestAgentCLIGenerateSurfacesError(t *testing.T) {
	bin := writeFakeAgent(t, `echo "boom" >&2; exit 3`)
	if _, err := (&AgentCLI{Argv: []string{bin}}).Generate(context.Background(), "hi"); err == nil {
		t.Fatal("expected error from a failing agent command")
	}
}

func TestAgentCLIEmbedUnsupported(t *testing.T) {
	a := NewAgentCLI("claude -p")
	if _, err := a.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("Embed should be unsupported for an agent CLI backend")
	}
	if a.SupportsEmbeddings() {
		t.Fatal("SupportsEmbeddings should be false")
	}
}

func TestAgentCLIAvailable(t *testing.T) {
	bin := writeFakeAgent(t, `echo ok`)
	if !(&AgentCLI{Argv: []string{bin}}).Available(context.Background()) {
		t.Fatal("absolute-path binary should be available")
	}
	if (&AgentCLI{Argv: []string{"prowl-nonexistent-binary-xyz"}}).Available(context.Background()) {
		t.Fatal("missing binary should be unavailable")
	}
	if (&AgentCLI{}).Available(context.Background()) {
		t.Fatal("empty argv should be unavailable")
	}
}
