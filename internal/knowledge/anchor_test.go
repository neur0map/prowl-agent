package knowledge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/parse/extract"
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
	FillMissingAnchorHashes(doc, root, nil)
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

// TestSymbolAnchorTracksLineShift proves a symbol-based anchor re-resolves its
// line range from the current source, so inserting lines above the symbol does
// not stale it, while changing the symbol's body does.
func TestSymbolAnchorTracksLineShift(t *testing.T) {
	root := t.TempDir()
	resolve := func(path string, data []byte, symbol string) (int, int, bool) {
		return extract.SymbolRange(path, data, symbol)
	}
	orig := "package foo\n\nfunc Foo() {\n\treturn\n}\n"
	if err := os.WriteFile(filepath.Join(root, "foo.go"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := &Document{Prowl: Metadata{Anchors: []Anchor{{Path: "foo.go", Symbol: "Foo"}}}}
	FillMissingAnchorHashes(doc, root, resolve)
	anchor := doc.Prowl.Anchors[0]
	if anchor.ContentHash == "" {
		t.Fatal("symbol anchor was not filled")
	}

	// Insert a line above the symbol: line range shifts, symbol body is unchanged.
	shifted := "package foo\n\nvar x = 1\n\nfunc Foo() {\n\treturn\n}\n"
	if err := os.WriteFile(filepath.Join(root, "foo.go"), []byte(shifted), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CheckAnchorResolved(root, anchor, resolve); got.Status != AnchorCurrent {
		t.Errorf("symbol anchor staled on line shift: status=%v msg=%q", got.Status, got.Message)
	}
	// A line-range anchor over the original range would false-stale here.
	rangeAnchor := Anchor{Path: "foo.go", LineStart: 3, LineEnd: 5, ContentHash: anchor.ContentHash}
	if got := CheckAnchorResolved(root, rangeAnchor, nil); got.Status != AnchorStale {
		t.Errorf("line-range control should have staled on shift: status=%v", got.Status)
	}

	// Change the symbol body: the symbol anchor must stale.
	changed := "package foo\n\nvar x = 1\n\nfunc Foo() {\n\tpanic(\"x\")\n}\n"
	if err := os.WriteFile(filepath.Join(root, "foo.go"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CheckAnchorResolved(root, anchor, resolve); got.Status != AnchorStale {
		t.Errorf("symbol anchor did not stale on body change: status=%v", got.Status)
	}
}
