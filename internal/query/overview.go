package query

import (
	"path"
	"sort"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// Cluster is a subsystem: either a connected group of files (via includes,
// exec/keybind chains, and shared resources) or, when a cohesive codebase forms
// one giant component, a directory grouping of it. Lang is the dominant language.
type Cluster struct {
	Label string   `json:"label"`
	Lang  string   `json:"lang"`
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
	langOf := make(map[string]string, len(files))
	for _, f := range files {
		uf.add(f.RelPath)
		langOf[f.RelPath] = f.Lang
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
		clusters = append(clusters, Cluster{Label: clusterLabel(members), Lang: dominantLang(members, langOf), Files: members})
	}
	clusters = splitBlobCluster(clusters, langOf)
	sortClusters(clusters)
	return clusters, nil
}

func sortClusters(clusters []Cluster) {
	sort.Slice(clusters, func(i, j int) bool {
		if len(clusters[i].Files) != len(clusters[j].Files) {
			return len(clusters[i].Files) > len(clusters[j].Files)
		}
		return clusters[i].Label < clusters[j].Label
	})
}

// splitBlobCluster detects a single dominant connected component (a cohesive
// codebase where nearly everything is one component) and subdivides it by
// directory subsystem, so clusters stay a useful onboarding map instead of one
// opaque blob. Repos with several balanced components (e.g. config include
// trees) are left unchanged.
func splitBlobCluster(clusters []Cluster, langOf map[string]string) []Cluster {
	if len(clusters) == 0 {
		return clusters
	}
	sortClusters(clusters)
	total := 0
	for _, c := range clusters {
		total += len(c.Files)
	}
	big := clusters[0]
	if len(big.Files) < 25 || len(big.Files)*100 < total*70 {
		return clusters
	}
	byDir := map[string][]string{}
	for _, m := range big.Files {
		byDir[subsystem(m)] = append(byDir[subsystem(m)], m)
	}
	if len(byDir) < 2 {
		return clusters
	}
	out := append([]Cluster(nil), clusters[1:]...)
	for dir, fs := range byDir {
		if len(fs) < 2 {
			continue
		}
		sort.Strings(fs)
		out = append(out, Cluster{Label: dir, Lang: dominantLang(fs, langOf), Files: fs})
	}
	return out
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

// dominantLang returns the most common language among a cluster's files.
func dominantLang(members []string, langOf map[string]string) string {
	counts := map[string]int{}
	for _, m := range members {
		counts[langOf[m]]++
	}
	best, bestN := "", 0
	for l, n := range counts {
		if n > bestN || (n == bestN && l < best) {
			best, bestN = l, n
		}
	}
	return best
}

// guideDocs picks the project's architecture/onboarding docs from indexed
// markdown, so an agent's first call points it at the human-written guides
// (README, ARCHITECTURE, CONTRIBUTING, docs/** guides) before it reads code.
// Ranked: root README, then other root guides, then docs/ guides; capped at 8.
func guideDocs(files []store.File) []string {
	type scored struct {
		path string
		rank int
	}
	var picks []scored
	rootGuides := map[string]bool{
		"architecture.md": true, "contributing.md": true, "development.md": true,
		"develop.md": true, "hacking.md": true, "design.md": true, "agents.md": true,
	}
	for _, f := range files {
		if f.Lang != "markdown" {
			continue
		}
		rel := f.RelPath
		base := strings.ToLower(path.Base(rel))
		lower := strings.ToLower(rel)
		depth := strings.Count(rel, "/")
		switch {
		case depth == 0 && base == "readme.md":
			picks = append(picks, scored{rel, 0})
		case depth == 0 && rootGuides[base]:
			picks = append(picks, scored{rel, 1})
		case (strings.HasPrefix(lower, "docs/") || strings.HasPrefix(lower, "doc/")) && matchesGuide(lower):
			picks = append(picks, scored{rel, 2})
		}
	}
	sort.Slice(picks, func(i, j int) bool {
		if picks[i].rank != picks[j].rank {
			return picks[i].rank < picks[j].rank
		}
		return picks[i].path < picks[j].path
	})
	out := make([]string, 0, len(picks))
	for _, p := range picks {
		if len(out) >= 8 {
			break
		}
		out = append(out, p.path)
	}
	return out
}

// matchesGuide reports whether a doc path looks like an architecture/onboarding
// guide rather than reference material.
func matchesGuide(s string) bool {
	for _, kw := range []string{
		"guide", "architecture", "develop", "contribut", "design", "overview",
		"getting-started", "getting_started", "structure", "codebase", "hacking", "onboard",
	} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// Overview is a compact map of the whole project for an agent's first call.
type Overview struct {
	Counts      store.Counts        `json:"counts"`
	Roles       map[string]int      `json:"roles"`
	Docs        []string            `json:"docs"`
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
	o.Docs = guideDocs(files)

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
