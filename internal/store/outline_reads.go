package store

// SymbolDocsInFile returns each symbol's raw leading doc comment for a file,
// keyed by symbol id. Outline joins this onto SymbolsInFile to state a symbol's
// purpose without a second per-symbol round trip. Only non-empty docs are
// returned, so the caller pays nothing for undocumented symbols. This lives in
// its own file (not graph.go) so the outline feature does not collide with the
// symbol-schema work that owns SymbolHit.
func (s *Store) SymbolDocsInFile(fileID int64) (map[int64]string, error) {
	rows, err := s.sql().Query(`SELECT id, IFNULL(doc,'') FROM symbols WHERE file_id=?`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	docs := map[int64]string{}
	for rows.Next() {
		var id int64
		var doc string
		if err := rows.Scan(&id, &doc); err != nil {
			return nil, err
		}
		if doc != "" {
			docs[id] = doc
		}
	}
	return docs, rows.Err()
}
