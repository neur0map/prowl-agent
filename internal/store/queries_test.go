package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestEdgeQueriesAndBlastRadius(t *testing.T) {
	s := openTmp(t)
	mk := func(p string) int64 {
		id, err := s.UpsertFile(File{RelPath: p, Lang: "generic", Hash: p, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b, c := mk("a.conf"), mk("b.conf"), mk("c.conf")

	if err := s.ReplaceFileGraph(a, nil, nil, []RawEdge{{Kind: "includes", Raw: "b.conf", Line: 1}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFileGraph(b, nil, nil, []RawEdge{{Kind: "includes", Raw: "c.conf", Line: 1}}, nil); err != nil {
		t.Fatal(err)
	}

	if un, _ := s.UnresolvedEdges(); len(un) != 2 {
		t.Fatalf("unresolved=%d want 2", len(un))
	}

	resolve := func(fileID, dst int64) {
		es, _ := s.EdgesFromFile(fileID, "includes")
		if len(es) != 1 {
			t.Fatalf("edges from %d = %d", fileID, len(es))
		}
		if err := s.SetEdgeResolved(es[0].ID, "file", dst); err != nil {
			t.Fatal(err)
		}
	}
	resolve(a, b)
	resolve(b, c)

	if un, _ := s.UnresolvedEdges(); len(un) != 0 {
		t.Fatalf("unresolved after=%d want 0", len(un))
	}
	if in, _ := s.IncomingEdges("file", b, "includes"); len(in) != 1 || in[0].File != "a.conf" {
		t.Fatalf("incoming b = %+v", in)
	}
	if out, _ := s.OutgoingEdges("file", a, "includes"); len(out) != 1 || !out[0].Resolved {
		t.Fatalf("outgoing a = %+v", out)
	}

	dep, err := s.TransitiveDependents(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(dep) != 2 || dep[0].File != "b.conf" || dep[0].Depth != 1 || dep[1].File != "a.conf" || dep[1].Depth != 2 {
		t.Fatalf("blast c = %+v", dep)
	}

	anc, _ := s.AncestorsToward(a)
	if len(anc) != 2 || anc[0].File != "b.conf" || anc[1].File != "c.conf" {
		t.Fatalf("ancestors a = %+v", anc)
	}

	cn, _ := s.Counts()
	if cn.Files != 3 || cn.Edges != 2 || cn.Resolved != 2 || cn.External != 0 || cn.Unresolved != 0 {
		t.Fatalf("counts = %+v", cn)
	}
}

// TestCountsResolutionSplit proves unresolved edges are classified honestly: an
// unresolved import in a module-language (Go) is an external dependency, not a
// broken reference, while an unresolved edge of another kind is a genuine gap.
func TestCountsResolutionSplit(t *testing.T) {
	s := openTmp(t)
	goFile, err := s.UpsertFile(File{RelPath: "main.go", Lang: "go", Hash: "g", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.UpsertFile(File{RelPath: "app.conf", Lang: "hyprlang", Hash: "c", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Go import of an external package: unresolved but expected (external).
	if err := s.ReplaceFileGraph(goFile, nil, nil, []RawEdge{{Kind: "includes", Raw: "fmt", Line: 1}}, nil); err != nil {
		t.Fatal(err)
	}
	// A keybind to a missing script: genuinely unresolved.
	if err := s.ReplaceFileGraph(cfg, nil, nil, []RawEdge{{Kind: "binds", Raw: "missing.sh", Line: 1}}, nil); err != nil {
		t.Fatal(err)
	}
	cn, err := s.Counts()
	if err != nil {
		t.Fatal(err)
	}
	if cn.External != 1 || cn.Unresolved != 1 {
		t.Fatalf("resolution split = external %d unresolved %d, want 1/1 (%+v)", cn.External, cn.Unresolved, cn)
	}
}

func TestImmediateGraphNeighborsEnforcesSQLLimit(t *testing.T) {
	s := openTmp(t)
	source, err := s.UpsertFile(File{RelPath: "source.go", Lang: "go", Hash: "source", Size: 1, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	var raw []RawEdge
	var targets []int64
	for i := 0; i < 20; i++ {
		path := fmt.Sprintf("target-%02d.go", i)
		id, err := s.UpsertFile(File{RelPath: path, Lang: "go", Hash: path, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		targets = append(targets, id)
		raw = append(raw, RawEdge{Kind: "includes", Raw: path, Line: i + 1})
	}
	if err := s.ReplaceFileGraph(source, nil, nil, raw, nil); err != nil {
		t.Fatal(err)
	}
	edges, err := s.EdgesFromFile(source, "includes")
	if err != nil || len(edges) != len(targets) {
		t.Fatalf("edges=%d err=%v", len(edges), err)
	}
	for i, edge := range edges {
		if err := s.SetEdgeResolved(edge.ID, "file", targets[i]); err != nil {
			t.Fatal(err)
		}
	}
	neighbors, err := s.ImmediateGraphNeighbors(source, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(neighbors) != 3 {
		t.Fatalf("neighbors=%d want SQL-bounded 3: %+v", len(neighbors), neighbors)
	}
	if neighbors[0].File != "target-00.go" || neighbors[2].File != "target-02.go" {
		t.Fatalf("neighbors not deterministically ordered: %+v", neighbors)
	}
	if neighbors, err := s.ImmediateGraphNeighbors(source, 0); err != nil || len(neighbors) != 0 {
		t.Fatalf("zero-limit neighbors=%+v err=%v", neighbors, err)
	}
}

func TestFirstChunksContextFiltersBeforeLimitAndHonorsCancellation(t *testing.T) {
	s := openTmp(t)
	for index := range 12 {
		path := fmt.Sprintf("a-empty-%02d.go", index)
		if _, err := s.UpsertFile(File{RelPath: path, Lang: "go", Hash: path, Size: 0, MTime: 1}); err != nil {
			t.Fatal(err)
		}
	}
	for index := range 5 {
		path := fmt.Sprintf("z-source-%02d.go", index)
		id, err := s.UpsertFile(File{RelPath: path, Lang: "go", Hash: path, Size: 16, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceFileGraph(id, nil, nil, nil, []Chunk{{StartLine: 1, EndLine: 1, Text: "package source"}}); err != nil {
			t.Fatal(err)
		}
	}

	chunks, err := s.FirstChunksContext(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 5 {
		t.Fatalf("chunks=%+v", chunks)
	}
	for index, chunk := range chunks {
		want := fmt.Sprintf("z-source-%02d.go", index)
		if chunk.File != want {
			t.Fatalf("chunk[%d].File=%q want %q", index, chunk.File, want)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.FirstChunksContext(ctx, 5); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query error=%v want context.Canceled", err)
	}
}
