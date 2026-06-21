package graph

import (
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestResolveJavaImports(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	mk := func(rel string) int64 {
		id, err := s.UpsertFile(store.File{RelPath: rel, Lang: "java", Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	store_ := mk("src/main/java/com/foo/Store.java")
	api := mk("src/main/java/com/foo/Api.java")

	// Api imports com.foo.Store (in-project, Maven layout) and java.util.List (JDK).
	if err := s.ReplaceFileGraph(api, nil, nil, []store.RawEdge{
		{Kind: "includes", Raw: "com.foo.Store", Line: 2},
		{Kind: "includes", Raw: "java.util.List", Line: 3},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(s); err != nil {
		t.Fatal(err)
	}

	if in, _ := s.IncomingEdges("file", store_, "includes"); len(in) != 1 || in[0].File != "src/main/java/com/foo/Api.java" {
		t.Fatalf("Store incoming = %+v, want one from Api.java", in)
	}
	dang, _ := s.UnresolvedEdges("includes")
	if len(dang) != 1 || dang[0].Raw != "java.util.List" {
		t.Fatalf("unresolved = %+v, want only java.util.List", dang)
	}
}
