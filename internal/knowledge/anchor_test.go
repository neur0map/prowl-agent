package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashRegionNormalizesLineEndings(t *testing.T) {
	lf, err := HashRegion([]byte("one\ntwo\nthree\n"), 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	crlf, err := HashRegion([]byte("one\r\ntwo\r\nthree\r\n"), 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if lf != crlf {
		t.Fatalf("line ending changed digest: lf=%s crlf=%s", lf, crlf)
	}
}

func TestCheckAnchorOnlyStalesForRegionChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.go")
	original := []byte("package source\n\nfunc Stable() {}\n\nfunc Other() {}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := HashRegion(original, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	anchor := Anchor{Path: "source.go", LineStart: 3, LineEnd: 3, ContentHash: hash}
	if got := CheckAnchor(root, anchor); got.Status != AnchorCurrent {
		t.Fatalf("initial status = %+v", got)
	}
	unrelated := []byte("package changed\n\nfunc Stable() {}\n\nfunc Other() {}\n")
	if err := os.WriteFile(path, unrelated, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CheckAnchor(root, anchor); got.Status != AnchorCurrent {
		t.Fatalf("unrelated change staled anchor: %+v", got)
	}
	changed := []byte("package changed\n\nfunc Changed() {}\n\nfunc Other() {}\n")
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CheckAnchor(root, anchor); got.Status != AnchorStale || got.Actual == got.Expected {
		t.Fatalf("region change status = %+v", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := CheckAnchor(root, anchor); got.Status != AnchorMissing {
		t.Fatalf("missing status = %+v", got)
	}
}

func TestCheckAnchorRejectsUnsafeAndOutOfRangeAnchors(t *testing.T) {
	root := t.TempDir()
	if got := CheckAnchor(root, Anchor{Path: "../secret", LineStart: 1, LineEnd: 1}); got.Status != AnchorInvalid {
		t.Fatalf("unsafe status = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(root, "one.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CheckAnchor(root, Anchor{Path: "one.txt", LineStart: 2, LineEnd: 3}); got.Status != AnchorInvalid {
		t.Fatalf("range status = %+v", got)
	}
}

// TestFillMissingAnchorHashes proves an author can anchor a claim with just a
// path and line range: the hash is computed from the current source, an existing
// hash is preserved, and an unreadable path is left for lint to report.
func TestFillMissingAnchorHashes(t *testing.T) {
	root := t.TempDir()
	body := "package foo\n\nfunc Foo() {}\n"
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "foo.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := &Document{Prowl: Metadata{Anchors: []Anchor{
		{Path: "pkg/foo.go", LineStart: 1, LineEnd: 1},
		{Path: "pkg/foo.go", LineStart: 3, LineEnd: 3, ContentHash: "sha256:keep"},
		{Path: "pkg/missing.go", LineStart: 1, LineEnd: 1},
	}}}
	FillMissingAnchorHashes(doc, root)
	want, err := HashRegion([]byte(body), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Prowl.Anchors[0].ContentHash; got != want {
		t.Errorf("hashless anchor = %q, want computed %q", got, want)
	}
	if got := doc.Prowl.Anchors[1].ContentHash; got != "sha256:keep" {
		t.Errorf("existing hash was overwritten: %q", got)
	}
	if got := doc.Prowl.Anchors[2].ContentHash; got != "" {
		t.Errorf("unreadable-path anchor got a hash: %q", got)
	}
}
