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
