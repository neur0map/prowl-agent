package store

import (
	"context"
	"database/sql"
	"errors"
)

var errInvalidOverviewLimit = errors.New("overview SQL limit must be positive")

func requirePositiveLimit(limit int) error {
	if limit <= 0 {
		return errInvalidOverviewLimit
	}
	return nil
}

// AllFilesContext returns every indexed file in deterministic path order while
// honoring cancellation.
func (s *Store) AllFilesContext(ctx context.Context) ([]File, error) {
	rows, err := s.sql().QueryContext(ctx, `SELECT id,rel_path,lang,IFNULL(role,''),size,hash,mtime,indexed_at FROM files ORDER BY rel_path`)
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

// FileDepEdgesContext returns every resolved file-to-file edge, optionally
// filtered by kind, in deterministic order while honoring cancellation.
func (s *Store) FileDepEdgesContext(ctx context.Context, kinds ...string) ([]FileEdge, error) {
	clause, args := inClause("e.kind", kinds)
	rows, err := s.sql().QueryContext(ctx, `SELECT sf.rel_path, e.file_id, df.rel_path, e.dst_id, e.kind, IFNULL(e.line,0)
		FROM edges e JOIN files sf ON sf.id=e.file_id JOIN files df ON df.id=e.dst_id
		WHERE e.resolved=1 AND e.dst_type='file'`+clause+` ORDER BY sf.rel_path, e.line, e.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileEdge
	for rows.Next() {
		var edge FileEdge
		if err := rows.Scan(&edge.SrcFile, &edge.SrcID, &edge.DstFile, &edge.DstID, &edge.Kind, &edge.Line); err != nil {
			return nil, err
		}
		out = append(out, edge)
	}
	return out, rows.Err()
}

// ResourceFileLinksContext returns every deterministic resource link while
// honoring cancellation.
func (s *Store) ResourceFileLinksContext(ctx context.Context) ([]FileEdge, error) {
	rows, err := s.sql().QueryContext(ctx, `SELECT uf.rel_path, e.file_id, df.rel_path, r.file_id
		FROM edges e JOIN resources r ON e.dst_type='resource' AND e.dst_id=r.id
		JOIN files uf ON uf.id=e.file_id JOIN files df ON df.id=r.file_id
		WHERE e.kind='uses_resource' AND e.resolved=1 AND r.file_id IS NOT NULL AND r.file_id<>e.file_id
		ORDER BY uf.rel_path, df.rel_path, e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileEdge
	for rows.Next() {
		edge := FileEdge{Kind: "resource"}
		if err := rows.Scan(&edge.SrcFile, &edge.SrcID, &edge.DstFile, &edge.DstID); err != nil {
			return nil, err
		}
		out = append(out, edge)
	}
	return out, rows.Err()
}

// ColorPaletteContext returns at most limit declared colors, sorted before capping.
func (s *Store) ColorPaletteContext(ctx context.Context, limit int) ([]ResourceRow, error) {
	if err := requirePositiveLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.sql().QueryContext(ctx, `SELECT min(r.id), r.kind, r.name, IFNULL(r.value,''), IFNULL(f.rel_path,''), IFNULL(r.line,0)
		FROM resources r LEFT JOIN files f ON f.id=r.file_id
		WHERE r.kind='color' AND r.name IS NOT NULL AND r.name<>'' GROUP BY r.name
		ORDER BY r.name LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResourceRow
	for rows.Next() {
		var row ResourceRow
		if err := rows.Scan(&row.ID, &row.Kind, &row.Name, &row.Value, &row.File, &row.Line); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// SymbolsByKindContext returns at most limit symbols of kind.
func (s *Store) SymbolsByKindContext(ctx context.Context, kind string, limit int) ([]SymbolHit, error) {
	if err := requirePositiveLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.sql().QueryContext(ctx, `SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line, sy.end_line
		FROM symbols sy JOIN files f ON f.id=sy.file_id WHERE sy.kind=?
		ORDER BY f.rel_path, sy.start_line, sy.id LIMIT ?`, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SymbolHit
	for rows.Next() {
		var hit SymbolHit
		if err := rows.Scan(&hit.ID, &hit.Name, &hit.Kind, &hit.Signature, &hit.File, &hit.Line, &hit.EndLine); err != nil {
			return nil, err
		}
		out = append(out, hit)
	}
	return out, rows.Err()
}

// CountSymbolsByKindContext counts symbols of kind in SQL, so a caller that only
// needs the total never materializes the rows.
func (s *Store) CountSymbolsByKindContext(ctx context.Context, kind string) (int, error) {
	var n int
	err := s.sql().QueryRowContext(ctx, `SELECT count(*) FROM symbols WHERE kind=?`, kind).Scan(&n)
	return n, err
}

// FanInContext returns at most limit fan-in rows, sorted before capping.
func (s *Store) FanInContext(ctx context.Context, limit int) ([]FanRow, error) {
	if err := requirePositiveLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.sql().QueryContext(ctx, `SELECT f.rel_path, count(*) c FROM edges e JOIN files f ON f.id=e.dst_id
		WHERE e.dst_type='file' AND e.resolved=1 AND e.kind<>'instantiates'
		GROUP BY e.dst_id ORDER BY c DESC, f.rel_path LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FanRow
	for rows.Next() {
		var row FanRow
		if err := rows.Scan(&row.File, &row.In); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// CountsContext computes exact index summary statistics while honoring
// cancellation. Counting is a single index scan per table: bounding it would
// only trade a correct total for a plausible-looking wrong one.
func (s *Store) CountsContext(ctx context.Context) (Counts, error) {
	counts := Counts{Langs: map[string]int{}}
	scalar := func(query string) (int, error) {
		var n int
		err := s.sql().QueryRowContext(ctx, query).Scan(&n)
		return n, err
	}
	var err error
	queries := []struct {
		dst *int
		sql string
	}{
		{&counts.Files, `SELECT count(*) FROM files`},
		{&counts.Symbols, `SELECT count(*) FROM symbols`},
		{&counts.Edges, `SELECT count(*) FROM edges`},
		{&counts.Resources, `SELECT count(*) FROM resources`},
		{&counts.Chunks, `SELECT count(*) FROM chunks`},
		{&counts.Resolved, `SELECT count(*) FROM edges WHERE resolved=1`},
	}
	for _, item := range queries {
		if *item.dst, err = scalar(item.sql); err != nil {
			return counts, err
		}
	}
	if counts.External, counts.Unresolved, err = s.edgeResolutionSplit(); err != nil {
		return counts, err
	}
	rows, err := s.sql().QueryContext(ctx, `SELECT lang, count(*) FROM files GROUP BY lang ORDER BY lang`)
	if err != nil {
		return counts, err
	}
	defer rows.Close()
	for rows.Next() {
		var lang string
		var n int
		if err := rows.Scan(&lang, &n); err != nil {
			return counts, err
		}
		counts.Langs[lang] = n
	}
	return counts, rows.Err()
}

// GetMetaContext returns metadata while honoring cancellation.
func (s *Store) GetMetaContext(ctx context.Context, key string) (string, error) {
	var value string
	err := s.sql().QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// RequirePublishedGenerationContext is the cancellation-aware publication check.
func (s *Store) RequirePublishedGenerationContext(ctx context.Context) error {
	state, err := s.GetMetaContext(ctx, "index_state")
	if err != nil {
		return err
	}
	if state != "complete" {
		return ErrGenerationIncomplete
	}
	return nil
}
