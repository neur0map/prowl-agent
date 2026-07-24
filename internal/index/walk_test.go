package index

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWalkSkipsUnsupportedSymlinkTargets(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.go"), []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "outside.go"), []byte("package outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	links := map[string]string{
		"directory-link":        external,
		"external-file-link.go": filepath.Join(external, "outside.go"),
		"inside-link.go":        "inside.go",
		"missing-link.go":       "missing.go",
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
	}

	want := []string{"inside-link.go", "inside.go"}
	t.Run("canonical", func(t *testing.T) {
		got, err := WalkContext(context.Background(), root, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("walk paths = %v, want %v", got, want)
		}
	})

	t.Run("bounded", func(t *testing.T) {
		snapshot, err := SourceSnapshotWithOptionsLimitContext(context.Background(), root, Options{}, len(want))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(snapshot.Paths, want) {
			t.Fatalf("snapshot paths = %v, want %v", snapshot.Paths, want)
		}
	})
}
