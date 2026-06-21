package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSignatureDetectsChanges(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\n")
	write("sub/b.go", "package sub\n")

	sig := func() uint64 {
		s, err := Signature(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	base := sig()
	if base == 0 {
		t.Fatal("signature should be non-zero for a populated tree")
	}
	// No change -> identical fingerprint.
	if sig() != base {
		t.Error("signature changed with no file change")
	}
	// Edit (bump mtime) -> changes.
	time.Sleep(10 * time.Millisecond)
	write("a.go", "package a\nvar X = 1\n")
	if afterEdit := sig(); afterEdit == base {
		t.Error("signature did not change after an edit")
	} else {
		base = afterEdit
	}
	// Add -> changes.
	write("c.go", "package a\n")
	if afterAdd := sig(); afterAdd == base {
		t.Error("signature did not change after adding a file")
	} else {
		base = afterAdd
	}
	// Delete -> changes.
	if err := os.Remove(filepath.Join(root, "c.go")); err != nil {
		t.Fatal(err)
	}
	if afterDel := sig(); afterDel == base {
		t.Error("signature did not change after deleting a file")
	}
}
