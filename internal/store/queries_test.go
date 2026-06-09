package store

import "testing"

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
