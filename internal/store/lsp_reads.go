package store

import "database/sql"

// GetFileByID returns the file row for an id and whether it exists.
func (s *Store) GetFileByID(id int64) (File, bool, error) {
	f, err := scanFile(s.db.QueryRow(
		`SELECT id,rel_path,lang,IFNULL(role,''),size,hash,mtime,indexed_at FROM files WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return File{}, false, nil
	}
	if err != nil {
		return File{}, false, err
	}
	return f, true, nil
}

// ResourceByID returns a single resource (with its file path) and whether it exists.
func (s *Store) ResourceByID(id int64) (ResourceRow, bool, error) {
	var r ResourceRow
	err := s.db.QueryRow(`
		SELECT r.id, r.kind, IFNULL(r.name,''), IFNULL(r.value,''), IFNULL(f.rel_path,''), IFNULL(r.line,0)
		FROM resources r LEFT JOIN files f ON f.id=r.file_id WHERE r.id=?`, id).
		Scan(&r.ID, &r.Kind, &r.Name, &r.Value, &r.File, &r.Line)
	if err == sql.ErrNoRows {
		return ResourceRow{}, false, nil
	}
	if err != nil {
		return ResourceRow{}, false, err
	}
	return r, true, nil
}

// ResourceDeclByName returns the first declared resource with the given name
// (the declaration the resolver links usages to) and whether one exists.
func (s *Store) ResourceDeclByName(name string) (ResourceRow, bool, error) {
	var r ResourceRow
	err := s.db.QueryRow(`
		SELECT r.id, r.kind, IFNULL(r.name,''), IFNULL(r.value,''), IFNULL(f.rel_path,''), IFNULL(r.line,0)
		FROM resources r LEFT JOIN files f ON f.id=r.file_id
		WHERE r.name=? ORDER BY r.id LIMIT 1`, name).
		Scan(&r.ID, &r.Kind, &r.Name, &r.Value, &r.File, &r.Line)
	if err == sql.ErrNoRows {
		return ResourceRow{}, false, nil
	}
	if err != nil {
		return ResourceRow{}, false, err
	}
	return r, true, nil
}

// ResourcesInFile lists named resources declared in a file, ordered by line.
func (s *Store) ResourcesInFile(fileID int64) ([]ResourceRow, error) {
	rows, err := s.db.Query(`
		SELECT r.id, r.kind, IFNULL(r.name,''), IFNULL(r.value,''), f.rel_path, IFNULL(r.line,0)
		FROM resources r JOIN files f ON f.id=r.file_id
		WHERE r.file_id=? AND r.name IS NOT NULL AND r.name<>'' ORDER BY r.line`, fileID)
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

// NamedResources returns distinct named resource declarations across the index,
// for completion. Capped at limit (0 means no cap).
func (s *Store) NamedResources(limit int) ([]ResourceRow, error) {
	q := `
		SELECT MIN(r.id), r.kind, r.name, IFNULL(MIN(r.value),''), '', IFNULL(MIN(r.line),0)
		FROM resources r
		WHERE r.name IS NOT NULL AND r.name<>''
		GROUP BY r.name, r.kind ORDER BY r.name`
	var args []any
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
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

// SymbolsLike returns symbols whose name contains sub (for workspace symbol and
// completion), ordered by name and capped at limit.
func (s *Store) SymbolsLike(sub string, limit int) ([]SymbolHit, error) {
	return s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line
		FROM symbols sy JOIN files f ON f.id=sy.file_id
		WHERE sy.name LIKE '%'||?||'%' ORDER BY sy.name LIMIT ?`, sub, limit)
}
