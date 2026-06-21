package graph

import (
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestResolveRustCrateImports(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	mk := func(rel string) int64 {
		id, err := s.UpsertFile(store.File{RelPath: rel, Lang: "rust", Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	store_ := mk("src/store.rs")
	mk("src/lib.rs")
	cli := mk("src/cli.rs")

	// cli uses crate::store (in-crate) and serde (external).
	if err := s.ReplaceFileGraph(cli, nil, nil, []store.RawEdge{
		{Kind: "includes", Raw: "crate::store::Store", Line: 1},
		{Kind: "includes", Raw: "serde::Deserialize", Line: 2},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(s); err != nil {
		t.Fatal(err)
	}

	// crate::store::Store resolved to the module file src/store.rs.
	if in, _ := s.IncomingEdges("file", store_, "includes"); len(in) != 1 || in[0].File != "src/cli.rs" {
		t.Fatalf("store.rs incoming = %+v, want one from cli.rs", in)
	}
	// External crate stays unresolved (informational).
	dang, _ := s.UnresolvedEdges("includes")
	if len(dang) != 1 || dang[0].Raw != "serde::Deserialize" {
		t.Fatalf("unresolved = %+v, want only serde::Deserialize", dang)
	}
}
