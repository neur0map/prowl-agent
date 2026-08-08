package store

import (
	"path/filepath"
	"testing"
)

func TestFileLookupNormalizesPath(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.UpsertFile(File{RelPath: "pkg/app.go", Lang: "go", Hash: "h", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	// An agent supplies "./pkg/app.go" or an uncleaned path; both must resolve.
	for _, p := range []string{"pkg/app.go", "./pkg/app.go", "pkg/../pkg/app.go"} {
		got, err := s.FileID(p)
		if err != nil || got != id {
			t.Fatalf("FileID(%q) = %d, %v; want %d", p, got, err, id)
		}
		f, ok, err := s.GetFileByPath(p)
		if err != nil || !ok || f.ID != id {
			t.Fatalf("GetFileByPath(%q) = %+v ok=%v err=%v; want id %d", p, f, ok, err, id)
		}
	}
}
