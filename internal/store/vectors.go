package store

import (
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
		if _, err := s.sql().Exec(`DROP TABLE IF EXISTS vec_chunks`); err != nil {
			return err
		}
	}
	q := fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(embedding float[%d])`, dim)
	if _, err := s.sql().Exec(q); err != nil {
		return err
	}
	if err := s.SetMeta("embed_dim", strconv.Itoa(dim)); err != nil {
		return err
	}
	return s.SetMeta("embed_model", model)
}

// VectorsInitialized reports whether the physical vec0 table exists. It is for
// embedding writers; readers must use VectorsReady.
func (s *Store) VectorsInitialized() bool {
	var n int
	err := s.sql().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='vec_chunks'`).Scan(&n)
	return err == nil && n > 0
}

// VectorsReady reports whether a usable vector index exists. Partial coverage
// counts: on a large repo the embedding backlog is rarely empty, and gating
// semantic search on a fully drained backlog switched it off for exactly the
// repos that need it most. Chunks that lack a vector are simply not vector-
// matched yet; lexical ranking still covers them.
func (s *Store) VectorsReady() bool {
	if !s.VectorsInitialized() {
		return false
	}
	var n int
	err := s.sql().QueryRow(`SELECT count(*) FROM (SELECT rowid FROM vec_chunks LIMIT 1)`).Scan(&n)
	return err == nil && n > 0
}

// VectorsComplete reports whether every indexed chunk has an embedding.
func (s *Store) VectorsComplete() bool {
	complete, err := s.GetMeta("vectors_complete")
	return err == nil && complete == "1" && s.VectorsInitialized()
}

// ResetVectors removes embeddings and their model/dimension metadata so every
// current chunk is re-embedded after an embedding model change.
func (s *Store) ResetVectors() error {
	if _, err := s.sql().Exec(`DROP TABLE IF EXISTS vec_chunks`); err != nil {
		return err
	}
	if err := s.SetMeta("embed_dim", ""); err != nil {
		return err
	}
	if err := s.SetMeta("embed_model", ""); err != nil {
		return err
	}
	return s.SetMeta("vectors_complete", "0")
}

// UpsertChunkVector stores (or replaces) the embedding for a chunk.
func (s *Store) UpsertChunkVector(chunkID int64, vec []float32) error {
	b, err := sqlite_vec.SerializeFloat32(vec)
	if err != nil {
		return err
	}
	_, err = s.sql().Exec(`INSERT OR REPLACE INTO vec_chunks(rowid, embedding) VALUES(?,?)`, chunkID, b)
	return err
}

// VectorSearch returns the k nearest chunks to vec, ordered by distance.
func (s *Store) VectorSearch(vec []float32, k int) ([]ChunkHit, error) {
	b, err := sqlite_vec.SerializeFloat32(vec)
	if err != nil {
		return nil, err
	}
	rows, err := s.sql().Query(`
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

// chunkHasContent excludes content-free chunks from embedding. Such a chunk
// embeds to the model's degenerate mean vector, which ranks nearer an arbitrary
// query than real code does, so a handful of them monopolize every KNN result.
// The chunker no longer emits them; this keeps indexes built by older versions
// from poisoning vector search until they are re-chunked.
// SQLite's one-argument trim() strips spaces only, so the whitespace set is given
// explicitly: tab, newline, carriage return, and space.
const chunkWhitespace = `' ' || char(9) || char(10) || char(13)`

const chunkHasContent = `trim(c.text, ` + chunkWhitespace + `) <> ''`

// ChunksWithoutVectors returns at most limit chunks that still need an embedding,
// lowest id first. A non-positive limit returns all of them. Embedding a large
// repo means walking ~100k chunks of source text, so callers window the backlog
// instead of materializing every pending chunk body at once. If the vec table
// does not exist yet, every chunk still needs one.
func (s *Store) ChunksWithoutVectors(limit int) ([]ChunkText, error) {
	q := `SELECT c.id, c.text FROM chunks c WHERE ` + chunkHasContent
	if s.VectorsInitialized() {
		q += ` AND c.id NOT IN (SELECT rowid FROM vec_chunks)`
	}
	q += ` ORDER BY c.id`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.sql().Query(q, args...)
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

// CountChunksWithoutVectors reports how much embedding work is still outstanding,
// so a caller can tell the user semantic search is still warming up.
func (s *Store) CountChunksWithoutVectors() (int, error) {
	q := `SELECT count(*) FROM chunks c WHERE ` + chunkHasContent
	if s.VectorsInitialized() {
		q += ` AND c.id NOT IN (SELECT rowid FROM vec_chunks)`
	}
	var n int
	err := s.sql().QueryRow(q).Scan(&n)
	return n, err
}

// CountEmbeddableChunks reports how many chunks are eligible for an embedding, so
// coverage is reported against the work that actually exists rather than against
// every chunk row.
func (s *Store) CountEmbeddableChunks() (int, error) {
	var n int
	err := s.sql().QueryRow(`SELECT count(*) FROM chunks c WHERE ` + chunkHasContent).Scan(&n)
	return n, err
}

// PruneContentFreeVectors drops vectors stored for content-free chunks by an
// older version, so an existing index recovers without a full re-embed. Returns
// the number removed.
func (s *Store) PruneContentFreeVectors() (int, error) {
	if !s.VectorsInitialized() {
		return 0, nil
	}
	res, err := s.sql().Exec(`DELETE FROM vec_chunks WHERE rowid IN
		(SELECT c.id FROM chunks c WHERE NOT (` + chunkHasContent + `))`)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// deleteChunkVectors removes vectors for a file's chunks (if the table exists),
// keeping the vector index consistent when chunks are replaced or deleted.
func deleteChunkVectors(tx writeRunner, fileID int64) error {
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='vec_chunks'`).Scan(&n); err != nil || n == 0 {
		return nil
	}
	_, err := tx.Exec(`DELETE FROM vec_chunks WHERE rowid IN (SELECT id FROM chunks WHERE file_id=?)`, fileID)
	return err
}
