package store

import (
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
	if cn.Files != 3 || cn.Edges != 2 || cn.Resolved != 2 || cn.Dangling != 0 {
		t.Fatalf("counts = %+v", cn)
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
