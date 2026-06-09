package query

import (
	"sort"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// Cluster is a connected group of files (a subsystem).
type Cluster struct {
	Label string   `json:"label"`
	Files []string `json:"files"`
}

// Clusters groups files connected through includes, exec/keybind chains, and
// shared resources into subsystems via connected components. Singletons are
// omitted. Clusters are ordered by size, then label.
func (q *Querier) Clusters() ([]Cluster, error) {
	files, err := q.s.AllFiles()
	if err != nil {
		return nil, err
	}
	uf := newUnionFind()
	for _, f := range files {
		uf.add(f.RelPath)
	}
	dep, err := q.s.FileDepEdges()
	if err != nil {
		return nil, err
	}
	for _, e := range dep {
		uf.union(e.SrcFile, e.DstFile)
	}
	res, err := q.s.ResourceFileLinks()
	if err != nil {
		return nil, err
	}
	for _, e := range res {
		uf.union(e.SrcFile, e.DstFile)
	}

	var clusters []Cluster
	for _, members := range uf.groups() {
		if len(members) < 2 {
			continue
		}
		sort.Strings(members)
		clusters = append(clusters, Cluster{Label: clusterLabel(members), Files: members})
	}
	sort.Slice(clusters, func(i, j int) bool {
		if len(clusters[i].Files) != len(clusters[j].Files) {
			return len(clusters[i].Files) > len(clusters[j].Files)
		}
		return clusters[i].Label < clusters[j].Label
	})
	return clusters, nil
}

// clusterLabel names a cluster by the most common top-level path segment.
func clusterLabel(members []string) string {
	counts := map[string]int{}
	for _, m := range members {
		seg := m
		if i := strings.IndexByte(m, '/'); i >= 0 {
			seg = m[:i]
		}
		counts[seg]++
	}
	segs := make([]string, 0, len(counts))
	for s := range counts {
		segs = append(segs, s)
	}
	sort.Strings(segs)
	best, bestN := "", 0
	for _, s := range segs {
		if counts[s] > bestN {
			best, bestN = s, counts[s]
		}
	}
	if best == "" || strings.Contains(best, ".") {
		return "misc"
	}
	return best
}

// Overview is a compact map of the whole project for an agent's first call.
type Overview struct {
	Counts      store.Counts        `json:"counts"`
	Roles       map[string]int      `json:"roles"`
	Entrypoints []string            `json:"entrypoints"`
	Clusters    []Cluster           `json:"clusters"`
	Palette     []store.ResourceRow `json:"palette"`
	Keybinds    int                 `json:"keybinds"`
	Hotspots    []store.FanRow      `json:"hotspots"`
}

// Overview assembles a high-level map of the project from the graph.
func (q *Querier) Overview() (Overview, error) {
	var o Overview
	c, err := q.s.Counts()
	if err != nil {
		return o, err
	}
	o.Counts = c

	files, err := q.s.AllFiles()
	if err != nil {
		return o, err
	}
	o.Roles = map[string]int{}
	for _, f := range files {
		role := f.Role
		if role == "" {
			role = "other"
		}
		o.Roles[role]++
	}

	// Entrypoints: files that depend on others but nothing depends on them.
	dep, err := q.s.FileDepEdges()
	if err != nil {
		return o, err
	}
	hasIncoming := map[string]bool{}
	hasOutgoing := map[string]bool{}
	for _, e := range dep {
		hasOutgoing[e.SrcFile] = true
		hasIncoming[e.DstFile] = true
	}
	for f := range hasOutgoing {
		if !hasIncoming[f] {
			o.Entrypoints = append(o.Entrypoints, f)
		}
	}
	sort.Strings(o.Entrypoints)

	clusters, err := q.Clusters()
	if err != nil {
		return o, err
	}
	if len(clusters) > 8 {
		clusters = clusters[:8]
	}
	o.Clusters = clusters

	o.Palette, err = q.s.ColorPalette()
	if err != nil {
		return o, err
	}
	kb, err := q.s.SymbolsByKind("keybind")
	if err != nil {
		return o, err
	}
	o.Keybinds = len(kb)
	o.Hotspots, err = q.s.FanIn(5)
	if err != nil {
		return o, err
	}
	return o, nil
}

// unionFind is a small string-keyed disjoint-set.
type unionFind struct {
	parent map[string]string
	size   map[string]int
}

func newUnionFind() *unionFind {
	return &unionFind{parent: map[string]string{}, size: map[string]int{}}
}

func (u *unionFind) add(x string) {
	if _, ok := u.parent[x]; !ok {
		u.parent[x] = x
		u.size[x] = 1
	}
}

func (u *unionFind) find(x string) string {
	u.add(x)
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]]
		x = u.parent[x]
	}
	return x
}

func (u *unionFind) union(a, b string) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	if u.size[ra] < u.size[rb] {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	u.size[ra] += u.size[rb]
}

func (u *unionFind) groups() map[string][]string {
	g := map[string][]string{}
	for x := range u.parent {
		r := u.find(x)
		g[r] = append(g[r], x)
	}
	return g
}
