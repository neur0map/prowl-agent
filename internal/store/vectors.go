package store

import (
	"database/sql"
	"fmt"
	"strconv"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

// init registers the sqlite-vec extension on all future SQLite connections.
func init() { sqlite_vec.Auto() }

// ChunkText is a chunk id paired with its text, for embedding.
type ChunkText struct {
	ID   int64
	Text string
}

// EnableVectors creates the vec0 table for the given embedding dimension,
// recreating it if the dimension changed (e.g. a different embedding model).
func (s *Store) EnableVectors(dim int, model string) error {
	if cur, _ := s.GetMeta("embed_dim"); cur != "" && cur != strconv.Itoa(dim) {
		if _, err := s.db.Exec(`DROP TABLE IF EXISTS vec_chunks`); err != nil {
			return err
		}
	}
	q := fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(embedding float[%d])`, dim)
	if _, err := s.db.Exec(q); err != nil {
		return err
	}
	if err := s.SetMeta("embed_dim", strconv.Itoa(dim)); err != nil {
		return err
	}
	return s.SetMeta("embed_model", model)
}

// VectorsReady reports whether the vec0 table exists.
func (s *Store) VectorsReady() bool {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='vec_chunks'`).Scan(&n)
	return err == nil && n > 0
}

// UpsertChunkVector stores (or replaces) the embedding for a chunk.
func (s *Store) UpsertChunkVector(chunkID int64, vec []float32) error {
	b, err := sqlite_vec.SerializeFloat32(vec)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO vec_chunks(rowid, embedding) VALUES(?,?)`, chunkID, b)
	return err
}

// VectorSearch returns the k nearest chunks to vec, ordered by distance.
func (s *Store) VectorSearch(vec []float32, k int) ([]ChunkHit, error) {
	b, err := sqlite_vec.SerializeFloat32(vec)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
		SELECT f.rel_path, c.start_line, c.end_line, substr(c.text,1,160)
		FROM vec_chunks v JOIN chunks c ON c.id=v.rowid JOIN files f ON f.id=c.file_id
		WHERE v.embedding MATCH ? AND k = ? ORDER BY v.distance`, b, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChunkHit
	for rows.Next() {
		var h ChunkHit
		if err := rows.Scan(&h.File, &h.StartLine, &h.EndLine, &h.Snippet); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ChunksWithoutVectors returns chunks that still need an embedding. If the vec
// table does not exist yet, every chunk is returned.
func (s *Store) ChunksWithoutVectors() ([]ChunkText, error) {
	q := `SELECT c.id, c.text FROM chunks c ORDER BY c.id`
	if s.VectorsReady() {
		q = `SELECT c.id, c.text FROM chunks c WHERE c.id NOT IN (SELECT rowid FROM vec_chunks) ORDER BY c.id`
	}
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChunkText
	for rows.Next() {
		var ct ChunkText
		if err := rows.Scan(&ct.ID, &ct.Text); err != nil {
			return nil, err
		}
		out = append(out, ct)
	}
	return out, rows.Err()
}

// deleteChunkVectors removes vectors for a file's chunks (if the table exists),
// keeping the vector index consistent when chunks are replaced or deleted.
func deleteChunkVectors(tx *sql.Tx, fileID int64) error {
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='vec_chunks'`).Scan(&n); err != nil || n == 0 {
		return nil
	}
	_, err := tx.Exec(`DELETE FROM vec_chunks WHERE rowid IN (SELECT id FROM chunks WHERE file_id=?)`, fileID)
	return err
}
