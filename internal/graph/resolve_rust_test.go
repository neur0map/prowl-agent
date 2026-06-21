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

func TestResolveRustWorkspaceAndMod(t *testing.T) {
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
	// A Cargo workspace: crate "searcher" with lib.rs declaring `mod searcher;`
	// and the submodule using crate::sink.
	lib := mk("crates/searcher/src/lib.rs")
	sub := mk("crates/searcher/src/searcher.rs")
	sink := mk("crates/searcher/src/sink.rs")

	if err := s.ReplaceFileGraph(lib, nil, nil, []store.RawEdge{
		{Kind: "includes", Raw: "mod::searcher", Line: 1},
		{Kind: "includes", Raw: "mod::sink", Line: 2},
	}, nil); err != nil {
		t.Fatal(err)
	}
	// crate:: resolves relative to the crate root (crates/searcher/src/), not repo root.
	if err := s.ReplaceFileGraph(sub, nil, nil, []store.RawEdge{
		{Kind: "includes", Raw: "crate::sink::Sink", Line: 1},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(s); err != nil {
		t.Fatal(err)
	}

	// `mod searcher;` and `mod sink;` in lib.rs resolve to the sibling files.
	in, _ := s.IncomingEdges("file", sub, "includes")
	if len(in) != 1 || in[0].File != "crates/searcher/src/lib.rs" {
		t.Fatalf("searcher.rs incoming = %+v, want one from lib.rs (mod decl)", in)
	}
	// sink.rs is included by lib.rs (mod) and by searcher.rs (crate::sink).
	inSink, _ := s.IncomingEdges("file", sink, "includes")
	got := map[string]bool{}
	for _, e := range inSink {
		got[e.File] = true
	}
	if !got["crates/searcher/src/lib.rs"] || !got["crates/searcher/src/searcher.rs"] {
		t.Fatalf("sink.rs incoming = %+v, want lib.rs (mod) and searcher.rs (crate::)", inSink)
	}
}
