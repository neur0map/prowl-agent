package store

import "database/sql"

// FirstChunk returns the first indexed chunk for a project-relative file.
func (s *Store) FirstChunk(relPath string) (ChunkBody, bool, error) {
	var chunk ChunkBody
	err := s.sql().QueryRow(`SELECT f.rel_path,c.start_line,c.text FROM chunks c JOIN files f ON f.id=c.file_id WHERE f.rel_path=? ORDER BY c.start_line LIMIT 1`, relPath).
		Scan(&chunk.File, &chunk.StartLine, &chunk.Text)
	if err == sql.ErrNoRows {
		return ChunkBody{}, false, nil
	}
	return chunk, err == nil, err
}

// ChunkAt returns the chunk beginning at a specific line in a file.
func (s *Store) ChunkAt(relPath string, startLine int) (ChunkBody, bool, error) {
	var chunk ChunkBody
	err := s.sql().QueryRow(`SELECT f.rel_path,c.start_line,c.text FROM chunks c JOIN files f ON f.id=c.file_id WHERE f.rel_path=? AND c.start_line=? LIMIT 1`, relPath, startLine).
		Scan(&chunk.File, &chunk.StartLine, &chunk.Text)
	if err == sql.ErrNoRows {
		return ChunkBody{}, false, nil
	}
	return chunk, err == nil, err
}
