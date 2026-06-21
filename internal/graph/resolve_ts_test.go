package graph

import (
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestResolveTSRelativeImports(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	mk := func(rel, lang string) int64 {
		id, err := s.UpsertFile(store.File{RelPath: rel, Lang: lang, Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	util := mk("src/util.ts", "typescript")
	btn := mk("src/components/btn.tsx", "tsx")
	app := mk("src/app.tsx", "tsx")

	// btn imports ../util (relative, resolvable) and react (package, external).
	if err := s.ReplaceFileGraph(btn, nil, nil, []store.RawEdge{
		{Kind: "includes", Raw: "../util", Line: 1},
		{Kind: "includes", Raw: "react", Line: 2},
	}, nil); err != nil {
		t.Fatal(err)
	}
	// app imports ./components/btn (relative, resolvable to a .tsx).
	if err := s.ReplaceFileGraph(app, nil, nil, []store.RawEdge{
		{Kind: "includes", Raw: "./components/btn", Line: 1},
	}, nil); err != nil {
		t.Fatal(err)
	}

	if err := Resolve(s); err != nil {
		t.Fatal(err)
	}

	// ../util resolved to src/util.ts (extensionless relative import).
	if in, _ := s.IncomingEdges("file", util, "includes"); len(in) != 1 || in[0].File != "src/components/btn.tsx" {
		t.Fatalf("util incoming = %+v, want one from btn.tsx", in)
	}
	// ./components/btn resolved to the .tsx file.
	if in, _ := s.IncomingEdges("file", btn, "includes"); len(in) != 1 || in[0].File != "src/app.tsx" {
		t.Fatalf("btn incoming = %+v, want one from app.tsx", in)
	}
	// The react package import stays unresolved (external).
	dang, _ := s.UnresolvedEdges("includes")
	if len(dang) != 1 || dang[0].Raw != "react" {
		t.Fatalf("unresolved = %+v, want only react", dang)
	}
}
