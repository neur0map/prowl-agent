package store

import "strings"

// SearchDocComments matches a query against symbol doc comments only (distinct
// from the external-docs `search_docs` surface). Doc prose is high-signal and
// short; scoring it inside its enclosing chunk lets thirty lines of imports
// outweigh eight lines of English, so the doc comment gets its own FTS field,
// ranked here by bm25 over that field alone.
//
// The terms are ANDed rather than treated as one adjacent phrase: a doc that
// answers "keep secrets out of the log file" reads "secret redaction for logs",
// where the query words are present but not contiguous. Each term is quoted via
// ftsTerms so punctuation cannot trigger an FTS5 syntax error; a query that is
// all stopwords or too-short tokens falls back to the quoted phrase.
func (s *Store) SearchDocComments(query string, limit int) ([]SymbolHit, error) {
	match := strings.Join(ftsTerms(query), " AND ")
	if match == "" {
		match = ftsQuote(query)
	}
	return s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line, sy.end_line
		FROM fts_docs JOIN symbols sy ON sy.id = fts_docs.rowid JOIN files f ON f.id = sy.file_id
		WHERE fts_docs MATCH ? ORDER BY bm25(fts_docs), sy.id LIMIT ?`, match, limit)
}
