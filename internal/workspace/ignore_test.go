package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureDerivedIgnoredKeepsCanonicalKnowledgeTrackable(t *testing.T) {
	root := t.TempDir()
	original := "# user rules\n*.secret\n.prowl/\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDerivedIgnored(root); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDerivedIgnored(root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	got := string(data)
	if !strings.HasPrefix(got, original) {
		t.Fatalf("user ignore content changed: %q", got)
	}
	if strings.Count(got, derivedIgnoreStart) != 1 {
		t.Fatalf("managed block duplicated: %q", got)
	}
	for _, required := range []string{"!.prowl/", "!.prowl/knowledge/", "!.prowl/knowledge/**", ".prowl/index.db*", ".prowl/logs/"} {
		if !strings.Contains(got, required) {
			t.Fatalf("managed ignore missing %q: %q", required, got)
		}
	}
	for _, path := range []string{filepath.Join(root, ".prowl", "knowledge", "concept.md"), filepath.Join(root, ".prowl", "cache", "derived.bin")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := exec.Command("git", "-C", root, "init", "--quiet").Run(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", root, "check-ignore", ".prowl/knowledge/concept.md").Run(); err == nil {
		t.Fatal("canonical knowledge is still ignored")
	}
	if err := exec.Command("git", "-C", root, "check-ignore", ".prowl/cache/derived.bin").Run(); err != nil {
		t.Fatalf("derived cache is not ignored: %v", err)
	}
}

func TestEnsureDerivedIgnoredFreshProjectUsesSpecificPaths(t *testing.T) {
	root := t.TempDir()
	if err := EnsureDerivedIgnored(root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	got := string(data)
	if strings.Contains(got, "\n.prowl/\n") || strings.HasPrefix(got, ".prowl/\n") {
		t.Fatalf("fresh project broadly ignores canonical knowledge: %q", got)
	}
	for _, required := range []string{".prowl/index.db*", ".prowl/cache/", ".prowl/logs/", ".prowl/editor/"} {
		if !strings.Contains(got, required) {
			t.Fatalf("specific derived ignore missing %q: %q", required, got)
		}
	}
}
