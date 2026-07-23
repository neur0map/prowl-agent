package store

import "database/sql"

// ChunkAt returns the chunk beginning at a specific line in a file.
func (s *Store) ChunkAt(relPath string, startLine int) (ChunkBody, bool, error) {
	var chunk ChunkBody
	err := s.db.QueryRow(`SELECT f.rel_path,c.start_line,c.text FROM chunks c JOIN files f ON f.id=c.file_id WHERE f.rel_path=? AND c.start_line=? LIMIT 1`, relPath, startLine).
		Scan(&chunk.File, &chunk.StartLine, &chunk.Text)
	if err == sql.ErrNoRows {
		return ChunkBody{}, false, nil
	}
	return chunk, err == nil, err
}
