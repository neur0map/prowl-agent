package store

import (
	"sort"
	"strings"
)

// EdgeRow is a graph edge joined with its owning file path.
type EdgeRow struct {
	ID       int64  `json:"-"`
	SrcType  string `json:"src_type"`
	SrcID    int64  `json:"src_id"`
	DstType  string `json:"dst_type,omitempty"`
	DstID    int64  `json:"dst_id,omitempty"`
	Kind     string `json:"kind"`
	FileID   int64  `json:"-"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Resolved bool   `json:"resolved"`
	Raw      string `json:"raw,omitempty"`
}

const edgeCols = `e.id,e.src_type,e.src_id,IFNULL(e.dst_type,''),IFNULL(e.dst_id,0),e.kind,e.file_id,f.rel_path,IFNULL(e.line,0),e.resolved,IFNULL(e.raw,'')`

func (s *Store) scanEdges(q string, args ...any) ([]EdgeRow, error) {
	rows, err := s.sql().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EdgeRow
	for rows.Next() {
		var e EdgeRow
		if err := rows.Scan(&e.ID, &e.SrcType, &e.SrcID, &e.DstType, &e.DstID, &e.Kind, &e.FileID, &e.File, &e.Line, &e.Resolved, &e.Raw); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func inClause(col string, vals []string) (string, []any) {
	if len(vals) == 0 {
		return "", nil
	}
	ph := strings.TrimRight(strings.Repeat("?,", len(vals)), ",")
	args := make([]any, len(vals))
	for i, v := range vals {
		args[i] = v
	}
	return " AND " + col + " IN (" + ph + ")", args
}

// IncomingEdges returns edges pointing at (dstType,dstID), optionally filtered by kind.
func (s *Store) IncomingEdges(dstType string, dstID int64, kinds ...string) ([]EdgeRow, error) {
	clause, kargs := inClause("e.kind", kinds)
	q := `SELECT ` + edgeCols + ` FROM edges e JOIN files f ON f.id=e.file_id WHERE e.dst_type=? AND e.dst_id=?` + clause + ` ORDER BY f.rel_path, e.line`
	return s.scanEdges(q, append([]any{dstType, dstID}, kargs...)...)
}

// OutgoingEdges returns edges originating at (srcType,srcID), optionally filtered by kind.
func (s *Store) OutgoingEdges(srcType string, srcID int64, kinds ...string) ([]EdgeRow, error) {
	clause, kargs := inClause("e.kind", kinds)
	q := `SELECT ` + edgeCols + ` FROM edges e JOIN files f ON f.id=e.file_id WHERE e.src_type=? AND e.src_id=?` + clause + ` ORDER BY f.rel_path, e.line`
	return s.scanEdges(q, append([]any{srcType, srcID}, kargs...)...)
}

// EdgesFromFile returns all edges owned by a file (file_id), regardless of source node.
func (s *Store) EdgesFromFile(fileID int64, kinds ...string) ([]EdgeRow, error) {
	clause, kargs := inClause("e.kind", kinds)
	q := `SELECT ` + edgeCols + ` FROM edges e JOIN files f ON f.id=e.file_id WHERE e.file_id=?` + clause + ` ORDER BY e.line`
	return s.scanEdges(q, append([]any{fileID}, kargs...)...)
}

// UnresolvedEdges returns edges that did not resolve to a target, optionally filtered by kind.
func (s *Store) UnresolvedEdges(kinds ...string) ([]EdgeRow, error) {
	clause, kargs := inClause("e.kind", kinds)
	q := `SELECT ` + edgeCols + ` FROM edges e JOIN files f ON f.id=e.file_id WHERE e.resolved=0` + clause + ` ORDER BY f.rel_path, e.line`
	return s.scanEdges(q, kargs...)
}

// DeleteUnresolvedEdges removes unresolved edges of the given kinds. Used for
// kinds like QML instantiation, where a non-match means a built-in/external type
// rather than a broken reference, so they must not count as dangling.
func (s *Store) DeleteUnresolvedEdges(kinds ...string) error {
	if len(kinds) == 0 {
		return nil
	}
	ph := strings.TrimRight(strings.Repeat("?,", len(kinds)), ",")
	args := make([]any, len(kinds))
	for i, k := range kinds {
		args[i] = k
	}
	_, err := s.sql().Exec(`DELETE FROM edges WHERE resolved=0 AND kind IN (`+ph+`)`, args...)
	return err
}

// Dep is a file reachable in a graph traversal at a given depth.
type Dep struct {
	File  string `json:"file"`
	Depth int    `json:"depth"`
}

// blastKinds are the edge kinds traversed for dependency/impact analysis. "pkg"
// is the synthetic Go package-dependency edge (an importer to each file in the
// imported package), so impact and entrypoints work across Go packages.
var blastKinds = []string{"includes", "references", "execs", "binds", "autostarts", "instantiates", "uses", "pkg"}

// TransitiveDependents returns files that (transitively) depend on fileID, the
// blast radius. A dependent is a file that includes/execs/references it.
func (s *Store) TransitiveDependents(fileID int64) ([]Dep, error) {
	return s.traverseDeps(fileID, true)
}

// AncestorsToward returns files reachable upward from fileID via dependency
// edges (what this file includes/execs, transitively); used for entrypoints.
func (s *Store) AncestorsToward(fileID int64) ([]Dep, error) {
	return s.traverseDeps(fileID, false)
}

// ImmediateGraphNeighbors returns at most limit unique one-hop dependencies and
// dependents of fileID without loading or traversing the complete edge graph.
func (s *Store) ImmediateGraphNeighbors(fileID int64, limit int) ([]Dep, error) {
	if limit <= 0 {
		return nil, nil
	}
	clause, kindArgs := inClause("kind", blastKinds)
	forwardArgs := append([]any{fileID}, kindArgs...)
	reverseArgs := append([]any{fileID}, kindArgs...)
	args := append(append(forwardArgs, reverseArgs...), limit)
	rows, err := s.sql().Query(`
		SELECT DISTINCT f.rel_path
		FROM files f
		JOIN (
			SELECT dst_id AS id FROM edges
			WHERE file_id=? AND resolved=1 AND dst_type='file'`+clause+`
			UNION
			SELECT file_id AS id FROM edges
			WHERE dst_id=? AND resolved=1 AND dst_type='file'`+clause+`
		) neighbors ON neighbors.id=f.id
		ORDER BY f.rel_path
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dep
	for rows.Next() {
		var dep Dep
		if err := rows.Scan(&dep.File); err != nil {
			return nil, err
		}
		dep.Depth = 1
		out = append(out, dep)
	}
	return out, rows.Err()
}

// traverseDeps walks the dependency graph from fileID with an in-memory BFS.
// reverse=true follows incoming edges (who depends on fileID -> blast radius);
// reverse=false follows outgoing edges (what fileID depends on -> ancestors).
// Loading the edge set once and doing a visited-set BFS is far faster than a
// recursive SQL CTE, which re-expands nodes across the dense pkg-edge graph.
func (s *Store) traverseDeps(fileID int64, reverse bool) ([]Dep, error) {
	adj, err := s.depAdjacency(reverse)
	if err != nil {
		return nil, err
	}
	depth := map[int64]int{fileID: 0}
	queue := []int64{fileID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nb := range adj[cur] {
			if _, seen := depth[nb]; !seen {
				depth[nb] = depth[cur] + 1
				queue = append(queue, nb)
			}
		}
	}
	delete(depth, fileID)
	if len(depth) == 0 {
		return nil, nil
	}
	paths, err := s.filePathsByID()
	if err != nil {
		return nil, err
	}
	out := make([]Dep, 0, len(depth))
	for id, d := range depth {
		if p, ok := paths[id]; ok {
			out = append(out, Dep{File: p, Depth: d})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		return out[i].File < out[j].File
	})
	return out, nil
}

// depAdjacency loads resolved file->file dependency edges into an adjacency map.
// reverse=true keys by target (dst -> dependents); reverse=false keys by source
// (file -> what it depends on).
func (s *Store) depAdjacency(reverse bool) (map[int64][]int64, error) {
	clause, kargs := inClause("kind", blastKinds)
	rows, err := s.sql().Query(`SELECT file_id, dst_id FROM edges WHERE resolved=1 AND dst_type='file'`+clause, kargs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	adj := map[int64][]int64{}
	for rows.Next() {
		var from, to int64
		if err := rows.Scan(&from, &to); err != nil {
			return nil, err
		}
		if reverse {
			adj[to] = append(adj[to], from)
		} else {
			adj[from] = append(adj[from], to)
		}
	}
	return adj, rows.Err()
}

// filePathsByID maps every file id to its repo-relative path.
func (s *Store) filePathsByID() (map[int64]string, error) {
	rows, err := s.sql().Query(`SELECT id, rel_path FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]string{}
	for rows.Next() {
		var id int64
		var p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, err
		}
		m[id] = p
	}
	return m, rows.Err()
}

// SymbolsInFile lists symbols defined in a file.
func (s *Store) SymbolsInFile(fileID int64) ([]SymbolHit, error) {
	return s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line, sy.end_line
		FROM symbols sy JOIN files f ON f.id=sy.file_id WHERE sy.file_id=? ORDER BY sy.start_line`, fileID)
}

// ResourceRow mirrors a resources row with its file path.
type ResourceRow struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
	File  string `json:"file"`
	Line  int    `json:"line"`
}

// AllResources returns every resource (declarations and literals).
func (s *Store) AllResources() ([]ResourceRow, error) {
	rows, err := s.sql().Query(`
		SELECT r.id, r.kind, IFNULL(r.name,''), IFNULL(r.value,''), IFNULL(f.rel_path,''), IFNULL(r.line,0)
		FROM resources r LEFT JOIN files f ON f.id=r.file_id ORDER BY r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResourceRow
	for rows.Next() {
		var r ResourceRow
		if err := rows.Scan(&r.ID, &r.Kind, &r.Name, &r.Value, &r.File, &r.Line); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FilesByRole lists files of any of the given roles.
func (s *Store) FilesByRole(roles ...string) ([]File, error) {
	clause, args := inClause("role", roles)
	rows, err := s.sql().Query(`SELECT id,rel_path,lang,IFNULL(role,''),size,hash,mtime,indexed_at FROM files WHERE 1=1`+clause+` ORDER BY rel_path`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// OrphanFiles returns files of the given roles with no incoming dependency edge.
func (s *Store) OrphanFiles(roles ...string) ([]File, error) {
	clause, args := inClause("f.role", roles)
	q := `SELECT f.id,f.rel_path,f.lang,IFNULL(f.role,''),f.size,f.hash,f.mtime,f.indexed_at FROM files f
		WHERE NOT EXISTS (
			SELECT 1 FROM edges e WHERE e.dst_type='file' AND e.dst_id=f.id AND e.resolved=1
			  AND e.kind IN ('includes','execs','binds','autostarts','references')
		)` + clause + ` ORDER BY f.rel_path`
	rows, err := s.sql().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FanRow is a file ranked by incoming dependency count.
type FanRow struct {
	File string `json:"file"`
	In   int    `json:"in"`
}

// kindNotIn builds an "AND e.kind NOT IN (...)" clause that always excludes
// "instantiates" (a non-dependency edge) plus any extra kinds passed.
func kindNotIn(extra ...string) (string, []any) {
	kinds := append([]string{"instantiates"}, extra...)
	ph := strings.TrimRight(strings.Repeat("?,", len(kinds)), ",")
	args := make([]any, len(kinds))
	for i, k := range kinds {
		args[i] = k
	}
	return " AND e.kind NOT IN (" + ph + ")", args
}

// FanIn returns files ranked by number of incoming resolved dependency edges,
// excluding "instantiates" and any extra kinds given (the doctor risk check
// passes "pkg" so normal Go package fan-in is not flagged as a risk).
func (s *Store) FanIn(limit int, exclude ...string) ([]FanRow, error) {
	clause, args := kindNotIn(exclude...)
	rows, err := s.sql().Query(`
		SELECT f.rel_path, count(*) c FROM edges e JOIN files f ON f.id=e.dst_id
		WHERE e.dst_type='file' AND e.resolved=1`+clause+` GROUP BY e.dst_id ORDER BY c DESC, f.rel_path LIMIT ?`,
		append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FanRow
	for rows.Next() {
		var r FanRow
		if err := rows.Scan(&r.File, &r.In); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Counts is an index summary for status().
type Counts struct {
	Files     int            `json:"files"`
	Symbols   int            `json:"symbols"`
	Edges     int            `json:"edges"`
	Resources int            `json:"resources"`
	Chunks    int            `json:"chunks"`
	Resolved  int            `json:"resolved_edges"`
	Dangling  int            `json:"dangling_edges"`
	Langs     map[string]int `json:"langs"`
}

// Counts computes index summary statistics.
func (s *Store) Counts() (Counts, error) {
	c := Counts{Langs: map[string]int{}}
	scalar := func(q string) (int, error) {
		var n int
		err := s.sql().QueryRow(q).Scan(&n)
		return n, err
	}
	var err error
	if c.Files, err = scalar(`SELECT count(*) FROM files`); err != nil {
		return c, err
	}
	if c.Symbols, err = scalar(`SELECT count(*) FROM symbols`); err != nil {
		return c, err
	}
	if c.Edges, err = scalar(`SELECT count(*) FROM edges`); err != nil {
		return c, err
	}
	if c.Resources, err = scalar(`SELECT count(*) FROM resources`); err != nil {
		return c, err
	}
	if c.Chunks, err = scalar(`SELECT count(*) FROM chunks`); err != nil {
		return c, err
	}
	if c.Resolved, err = scalar(`SELECT count(*) FROM edges WHERE resolved=1`); err != nil {
		return c, err
	}
	if c.Dangling, err = scalar(`SELECT count(*) FROM edges WHERE resolved=0`); err != nil {
		return c, err
	}
	rows, err := s.sql().Query(`SELECT lang, count(*) FROM files GROUP BY lang`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var lang string
		var n int
		if err := rows.Scan(&lang, &n); err != nil {
			return c, err
		}
		c.Langs[lang] = n
	}
	return c, rows.Err()
}

// ResetResolution clears all edge resolution so a fresh global pass can run. It
// also drops the synthetic "pkg" edges (Go package dependencies), which the
// resolve pass rebuilds, so they never accumulate across re-resolves.
func (s *Store) ResetResolution() error {
	if _, err := s.sql().Exec(`DELETE FROM edges WHERE kind='pkg'`); err != nil {
		return err
	}
	_, err := s.sql().Exec(`UPDATE edges SET resolved=0, dst_type=NULL, dst_id=NULL`)
	return err
}

// PkgEdge is a synthetic Go package dependency: the importing file depends on
// one file of the imported package.
type PkgEdge struct {
	FileID    int64
	DstFileID int64
	Line      int
	Raw       string
}

// AddPackageEdges inserts resolved "pkg" file-to-file edges in one transaction.
func (s *Store) AddPackageEdges(es []PkgEdge) error {
	if len(es) == 0 {
		return nil
	}
	return s.writeTransaction(func(tx writeRunner) error {
		for _, e := range es {
			if _, err := tx.Exec(
				`INSERT INTO edges(src_type,src_id,dst_type,dst_id,kind,file_id,line,resolved,raw)
				 VALUES('file',?,'file',?,'pkg',?,?,1,?)`,
				e.FileID, e.DstFileID, e.FileID, e.Line, e.Raw); err != nil {
				return err
			}
		}
		return nil
	})
}

// NamespaceFiles maps each declared namespace to the file ids that declare it,
// for resolving C# `using` imports to the files of the imported namespace.
func (s *Store) NamespaceFiles() (map[string][]int64, error) {
	rows, err := s.sql().Query(`SELECT name, file_id FROM resources WHERE kind='namespace' AND file_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string][]int64{}
	for rows.Next() {
		var name string
		var fid int64
		if err := rows.Scan(&name, &fid); err != nil {
			return nil, err
		}
		m[name] = append(m[name], fid)
	}
	return m, rows.Err()
}

// SetEdgeResolved points an edge at a resolved target.
func (s *Store) SetEdgeResolved(edgeID int64, dstType string, dstID int64) error {
	_, err := s.sql().Exec(`UPDATE edges SET resolved=1, dst_type=?, dst_id=? WHERE id=?`, dstType, dstID, edgeID)
	return err
}
