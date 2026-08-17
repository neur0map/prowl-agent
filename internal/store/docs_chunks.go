package store

import "strconv"

// ChunksByDocMatch maps doc-comment matches onto the chunk that contains each
// matching symbol, so a doc hit fuses into the same ranked chunk list as text
// and vector hits -- deduped on the same file:start_line key those lists use.
// It exists so `search` can surface a chunk whose *purpose* answers the query
// even when the chunk's *code* shares no query token: the redact.py case, a
// chunk 22% docstring and 78% imports, that BM25-over-chunk-text buried below
// fifty results.
//
// Matching mirrors SearchChunks' precision tiers (exact phrase, then all terms,
// then any term via ftsMatchTiers), NOT SearchDocs' strict AND. A concept query
// like "keep secrets out of the log file" overlaps the answering docstring
// ("secret redaction for logs ... reach log files") on only a few terms, so a
// strict AND of every term returns nothing; the recall that makes this feature
// fire lives in the OR tier. bm25(fts_docs) orders within each tier -- the doc
// prose alone, never diluted by the enclosing code -- and hits are deduped by
// chunk and capped at limit, matching SearchChunks' shape.
func (s *Store) ChunksByDocMatch(query string, limit int) ([]ChunkHit, error) {
	seen := map[string]bool{}
	var out []ChunkHit
	push := func(h ChunkHit) bool {
		key := h.File + "\x00" + strconv.Itoa(h.StartLine)
		if seen[key] {
			return false
		}
		seen[key] = true
		out = append(out, h)
		return len(out) >= limit
	}
	for _, m := range ftsMatchTiers(query) {
		hits, err := s.chunksByDocMatch(m, limit)
		if err != nil {
			return nil, err
		}
		for _, h := range hits {
			if push(h) {
				return out, nil
			}
		}
	}
	return out, nil
}

// chunksByDocMatch runs one fts_docs MATCH and joins each matching symbol to the
// chunk whose line range contains the symbol's first line, so the result keys on
// the chunk's start line (fusion dedupes chunks, not symbols). Ordered by the doc
// field's own bm25, then chunk start line for a stable tie-break.
func (s *Store) chunksByDocMatch(match string, limit int) ([]ChunkHit, error) {
	rows, err := s.sql().Query(`
		SELECT f.rel_path, c.start_line, c.end_line, substr(c.text,1,160)
		FROM fts_docs
		JOIN symbols sy ON sy.id = fts_docs.rowid
		JOIN files f ON f.id = sy.file_id
		JOIN chunks c ON c.file_id = sy.file_id AND sy.start_line BETWEEN c.start_line AND c.end_line
		WHERE fts_docs MATCH ? ORDER BY bm25(fts_docs), c.start_line LIMIT ?`, match, limit)
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
