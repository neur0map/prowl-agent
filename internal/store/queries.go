package store

import "strings"

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
	rows, err := s.db.Query(q, args...)
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

// Dep is a file reachable in a graph traversal at a given depth.
type Dep struct {
	File  string `json:"file"`
	Depth int    `json:"depth"`
}

// blastKinds are the edge kinds traversed for dependency/impact analysis.
var blastKinds = []string{"includes", "references", "execs", "binds", "autostarts"}

// TransitiveDependents returns files that (transitively) depend on fileID — the
// blast radius. A dependent is a file that includes/execs/references it.
func (s *Store) TransitiveDependents(fileID int64) ([]Dep, error) {
	clause, kargs := inClause("e.kind", blastKinds)
	q := `WITH RECURSIVE dep(id,depth) AS (
		SELECT ?,0
		UNION
		SELECT e.file_id, dep.depth+1 FROM edges e JOIN dep ON e.dst_type='file' AND e.dst_id=dep.id
		WHERE e.resolved=1` + clause + `
	)
	SELECT f.rel_path, min(dep.depth) FROM dep JOIN files f ON f.id=dep.id
	WHERE dep.id<>? GROUP BY dep.id ORDER BY 2,1`
	args := append([]any{fileID}, kargs...)
	args = append(args, fileID)
	return s.scanDeps(q, args...)
}

// AncestorsToward returns files reachable upward from fileID via dependency
// edges (what this file includes/execs, transitively) — used for entrypoints.
func (s *Store) AncestorsToward(fileID int64) ([]Dep, error) {
	clause, kargs := inClause("e.kind", blastKinds)
	q := `WITH RECURSIVE up(id,depth) AS (
		SELECT ?,0
		UNION
		SELECT e.dst_id, up.depth+1 FROM edges e JOIN up ON e.file_id=up.id AND e.dst_type='file'
		WHERE e.resolved=1` + clause + `
	)
	SELECT f.rel_path, min(up.depth) FROM up JOIN files f ON f.id=up.id
	WHERE up.id<>? GROUP BY up.id ORDER BY 2,1`
	args := append([]any{fileID}, kargs...)
	args = append(args, fileID)
	return s.scanDeps(q, args...)
}

func (s *Store) scanDeps(q string, args ...any) ([]Dep, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dep
	for rows.Next() {
		var d Dep
		if err := rows.Scan(&d.File, &d.Depth); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SymbolsInFile lists symbols defined in a file.
func (s *Store) SymbolsInFile(fileID int64) ([]SymbolHit, error) {
	return s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line
		FROM symbols sy JOIN files f ON f.id=sy.file_id WHERE sy.file_id=? ORDER BY sy.start_line`, fileID)
}

// ResourceRow mirrors a resources row with its file path.
type ResourceRow struct {
	ID    int64
	Kind  string
	Name  string
	Value string
	File  string
	Line  int
}

// AllResources returns every resource (declarations and literals).
func (s *Store) AllResources() ([]ResourceRow, error) {
	rows, err := s.db.Query(`
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
	rows, err := s.db.Query(`SELECT id,rel_path,lang,IFNULL(role,''),size,hash,mtime,indexed_at FROM files WHERE 1=1`+clause+` ORDER BY rel_path`, args...)
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
			  AND e.kind IN ('includes','execs','binds','autostarts')
		)` + clause + ` ORDER BY f.rel_path`
	rows, err := s.db.Query(q, args...)
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

// FanIn returns files ranked by number of incoming resolved dependency edges.
func (s *Store) FanIn(limit int) ([]FanRow, error) {
	rows, err := s.db.Query(`
		SELECT f.rel_path, count(*) c FROM edges e JOIN files f ON f.id=e.dst_id
		WHERE e.dst_type='file' AND e.resolved=1 GROUP BY e.dst_id ORDER BY c DESC, f.rel_path LIMIT ?`, limit)
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
		err := s.db.QueryRow(q).Scan(&n)
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
	rows, err := s.db.Query(`SELECT lang, count(*) FROM files GROUP BY lang`)
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

// ResetResolution clears all edge resolution so a fresh global pass can run.
func (s *Store) ResetResolution() error {
	_, err := s.db.Exec(`UPDATE edges SET resolved=0, dst_type=NULL, dst_id=NULL`)
	return err
}

// SetEdgeResolved points an edge at a resolved target.
func (s *Store) SetEdgeResolved(edgeID int64, dstType string, dstID int64) error {
	_, err := s.db.Exec(`UPDATE edges SET resolved=1, dst_type=?, dst_id=? WHERE id=?`, dstType, dstID, edgeID)
	return err
}
