package store

// MarkerCount is the number of chunks in one file whose stored text contains a
// marker.
type MarkerCount struct {
	File  string
	Count int
}

// ChunksContainingMarker returns, per file, how many stored chunks contain
// marker, ordered by path. Passing redact.Mask finds files whose secrets were
// masked at index time: the marker is the durable record of a removal, since the
// value itself was never stored.
func (s *Store) ChunksContainingMarker(marker string) ([]MarkerCount, error) {
	rows, err := s.sql().Query(`
		SELECT f.rel_path, count(*)
		FROM chunks c JOIN files f ON f.id=c.file_id
		WHERE instr(c.text, ?) > 0
		GROUP BY f.rel_path ORDER BY f.rel_path`, marker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MarkerCount
	for rows.Next() {
		var m MarkerCount
		if err := rows.Scan(&m.File, &m.Count); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
