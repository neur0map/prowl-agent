package store

// FanOut returns files ranked by number of outgoing resolved dependency edges.
func (s *Store) FanOut(limit int) ([]FanRow, error) {
	rows, err := s.db.Query(`
		SELECT f.rel_path, count(*) c FROM edges e JOIN files f ON f.id=e.file_id
		WHERE e.dst_type='file' AND e.resolved=1 GROUP BY e.file_id
		ORDER BY c DESC, f.rel_path LIMIT ?`, limit)
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

// SymbolsByKind returns all symbols of a given kind.
func (s *Store) SymbolsByKind(kind string) ([]SymbolHit, error) {
	return s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line
		FROM symbols sy JOIN files f ON f.id=sy.file_id WHERE sy.kind=? ORDER BY f.rel_path, sy.start_line`, kind)
}

// FileEdge is a resolved edge between two files (owning file -> target file).
type FileEdge struct {
	SrcFile string
	SrcID   int64
	DstFile string
	DstID   int64
	Kind    string
	Line    int
}

// FileDepEdges returns resolved file-to-file edges (owning file as source),
// optionally filtered by kind. Used for cycle and layer-crossing checks.
func (s *Store) FileDepEdges(kinds ...string) ([]FileEdge, error) {
	clause, args := inClause("e.kind", kinds)
	q := `SELECT sf.rel_path, e.file_id, df.rel_path, e.dst_id, e.kind, IFNULL(e.line,0)
		FROM edges e JOIN files sf ON sf.id=e.file_id JOIN files df ON df.id=e.dst_id
		WHERE e.resolved=1 AND e.dst_type='file'` + clause + ` ORDER BY sf.rel_path, e.line`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileEdge
	for rows.Next() {
		var e FileEdge
		if err := rows.Scan(&e.SrcFile, &e.SrcID, &e.DstFile, &e.DstID, &e.Kind, &e.Line); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// FileMetric carries per-file size signals for health checks.
type FileMetric struct {
	ID    int64
	File  string
	Role  string
	Size  int64
	Lines int
}

// FileMetrics returns size and line count (max chunk end line) per file.
func (s *Store) FileMetrics() ([]FileMetric, error) {
	rows, err := s.db.Query(`
		SELECT f.id, f.rel_path, IFNULL(f.role,''), f.size,
		       IFNULL((SELECT max(end_line) FROM chunks c WHERE c.file_id=f.id),0)
		FROM files f ORDER BY f.rel_path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileMetric
	for rows.Next() {
		var m FileMetric
		if err := rows.Scan(&m.ID, &m.File, &m.Role, &m.Size, &m.Lines); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ResourceFileLinks returns file pairs connected by a shared resource: a file
// that uses a resource (SrcFile) and the file that declares it (DstFile). Used
// to cluster files that share colors, fonts, or variables.
func (s *Store) ResourceFileLinks() ([]FileEdge, error) {
	rows, err := s.db.Query(`
		SELECT uf.rel_path, e.file_id, df.rel_path, r.file_id
		FROM edges e
		JOIN resources r ON e.dst_type='resource' AND e.dst_id=r.id
		JOIN files uf ON uf.id=e.file_id
		JOIN files df ON df.id=r.file_id
		WHERE e.kind='uses_resource' AND e.resolved=1 AND r.file_id IS NOT NULL AND r.file_id<>e.file_id
		ORDER BY uf.rel_path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileEdge
	for rows.Next() {
		e := FileEdge{Kind: "resource"}
		if err := rows.Scan(&e.SrcFile, &e.SrcID, &e.DstFile, &e.DstID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ColorPalette returns declared color resources (named) deduped by name.
func (s *Store) ColorPalette() ([]ResourceRow, error) {
	rows, err := s.db.Query(`
		SELECT r.id, r.kind, r.name, IFNULL(r.value,''), IFNULL(f.rel_path,''), IFNULL(r.line,0)
		FROM resources r LEFT JOIN files f ON f.id=r.file_id
		WHERE r.kind='color' AND r.name IS NOT NULL AND r.name<>'' GROUP BY r.name ORDER BY r.name`)
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
