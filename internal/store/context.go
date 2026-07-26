package store

import (
	"context"
	"database/sql"
)

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

// FirstChunksContext returns the first indexed chunk for up to limit files.
// Files without extracted chunks are filtered before the deterministic limit.
func (s *Store) FirstChunksContext(ctx context.Context, limit int) ([]ChunkBody, error) {
	if err := requirePositiveLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.sql().QueryContext(ctx, `
		SELECT f.rel_path, c.start_line, c.text
		FROM files f
		JOIN chunks c ON c.file_id = f.id
		WHERE c.id = (
			SELECT first.id
			FROM chunks first
			WHERE first.file_id = f.id
			ORDER BY first.start_line, first.id
			LIMIT 1
		)
		ORDER BY f.rel_path
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chunks := make([]ChunkBody, 0, limit)
	for rows.Next() {
		var chunk ChunkBody
		if err := rows.Scan(&chunk.File, &chunk.StartLine, &chunk.Text); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, rows.Err()
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
