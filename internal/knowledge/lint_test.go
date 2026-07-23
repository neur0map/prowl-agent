package knowledge_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/knowledge/okfv01"
)

func TestLintSurfacesHealthFindingsWithoutRejectingBundle(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, ".prowl", "knowledge")
	repo := knowledge.NewRepository(bundle, okfv01.Codec{})
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}
	source := []byte("one\nstable region\nthree\n")
	if err := os.WriteFile(filepath.Join(root, "source.txt"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, _ := knowledge.HashRegion(source, 2, 2)
	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(bundle, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.md", "---\ntype: Decision\ntitle: A\nresource: relative/path\nprowl:\n  id: duplicate\n  status: accepted\n  valid_from: 2026-08-01\n  valid_to: 2026-07-01\n  anchors:\n    - path: source.txt\n      line_start: 2\n      line_end: 2\n      content_hash: "+hash+"\n---\n[Missing](missing.md)\n")
	write("b.md", "---\ntype: FutureType\ntitle: B\nprowl:\n  id: duplicate\n  status: rejected\n---\nStandalone.\n")
	write("malformed.md", "---\ntitle: [broken\n---\n")
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("one\nchanged region\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := repo.Lint(root)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, finding := range findings {
		codes[finding.Code] = true
	}
	for _, code := range []string{
		"knowledge.broken_link", "knowledge.invalid_resource", "knowledge.invalid_temporal_range",
		"knowledge.stale_anchor", "knowledge.duplicate_id", "knowledge.contradiction",
		"knowledge.invalid_document", "knowledge.orphan",
	} {
		if !codes[code] {
			t.Errorf("missing finding %s in %+v", code, findings)
		}
	}
}

func TestLintAcceptsUnknownTypesAndCurrentEvidence(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, ".prowl", "knowledge")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	source := []byte("evidence\n")
	if err := os.WriteFile(filepath.Join(root, "source.txt"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, _ := knowledge.HashRegion(source, 1, 1)
	doc := "---\ntype: NewTypeFromFutureSpec\ntitle: Future\nprowl:\n  id: future\n  anchors:\n    - path: source.txt\n      line_start: 1\n      line_end: 1\n      content_hash: " + hash + "\n---\n"
	if err := os.WriteFile(filepath.Join(bundle, "future.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := knowledge.NewRepository(bundle, okfv01.Codec{})
	findings, err := repo.Lint(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Severity == "error" || finding.Code == "knowledge.stale_anchor" {
			t.Fatalf("healthy future document failed lint: %+v", findings)
		}
	}
}
