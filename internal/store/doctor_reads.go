package store

import "context"

// FanOut returns files ranked by number of outgoing resolved dependency edges,
// excluding "instantiates" and any extra kinds given.
func (s *Store) FanOut(limit int, exclude ...string) ([]FanRow, error) {
	clause, args := kindNotIn(exclude...)
	rows, err := s.sql().Query(`
		SELECT f.rel_path, count(*) c FROM edges e JOIN files f ON f.id=e.file_id
		WHERE e.dst_type='file' AND e.resolved=1`+clause+` GROUP BY e.file_id
		ORDER BY c DESC, f.rel_path LIMIT ?`, append(args, limit)...)
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
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line, sy.end_line
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
	return s.FileDepEdgesContext(context.Background(), kinds...)
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
	rows, err := s.sql().Query(`
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
	return s.ResourceFileLinksContext(context.Background())
}

// ColorPalette returns declared color resources (named) deduped by name.
func (s *Store) ColorPalette() ([]ResourceRow, error) {
	rows, err := s.sql().Query(`
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

// FuncSpan is a function or method ranked by its line span, a cheap,
// language-agnostic proxy for where complex, refactor-worthy code concentrates.
type FuncSpan struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Lines      int    `json:"lines,omitempty"`
	Complexity int    `json:"complexity,omitempty"`
}

// LargestFunctions returns function/method symbols with the widest line span,
// longest first. Single-line definitions are excluded.
func (s *Store) LargestFunctions(limit int) ([]FuncSpan, error) {
	rows, err := s.sql().Query(`
		SELECT sy.name, sy.kind, f.rel_path, sy.start_line,
		       (sy.end_line - sy.start_line + 1) AS lines
		FROM symbols sy JOIN files f ON f.id=sy.file_id
		WHERE sy.kind IN ('function','method') AND sy.end_line > sy.start_line
		ORDER BY lines DESC, f.rel_path, sy.start_line LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FuncSpan
	for rows.Next() {
		var fs FuncSpan
		if err := rows.Scan(&fs.Name, &fs.Kind, &fs.File, &fs.Line, &fs.Lines); err != nil {
			return nil, err
		}
		out = append(out, fs)
	}
	return out, rows.Err()
}

// MostComplex returns function/method symbols ranked by cyclomatic-style
// complexity, highest first. Trivial functions (complexity <= 1) and languages
// where complexity is not computed are excluded.
func (s *Store) MostComplex(limit int) ([]FuncSpan, error) {
	rows, err := s.sql().Query(`
		SELECT sy.name, sy.kind, f.rel_path, sy.start_line, sy.complexity
		FROM symbols sy JOIN files f ON f.id=sy.file_id
		WHERE sy.kind IN ('function','method') AND sy.complexity > 1
		ORDER BY sy.complexity DESC, f.rel_path, sy.start_line LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FuncSpan
	for rows.Next() {
		var fs FuncSpan
		if err := rows.Scan(&fs.Name, &fs.Kind, &fs.File, &fs.Line, &fs.Complexity); err != nil {
			return nil, err
		}
		out = append(out, fs)
	}
	return out, rows.Err()
}
