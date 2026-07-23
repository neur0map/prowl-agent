package store

import "strings"

// Symbol is a definition to insert (parent linked by name within the file).
type Symbol struct {
	Name, Kind, Signature string
	StartLine, EndLine    int
	ParentName            string
	Complexity            int
}

// Resource is a shared value (color/font/path/var) to insert.
type Resource struct {
	Kind, Name, Value string
	Line              int
}

// RawEdge is an unresolved edge: dst is the raw string until resolution runs.
// SrcName (if matching a symbol in the same file) makes the edge symbol-sourced;
// otherwise it is file-sourced.
type RawEdge struct {
	SrcName string
	Kind    string
	Raw     string
	Line    int
}

// Chunk is a text window for FTS (and future embeddings).
type Chunk struct {
	StartLine, EndLine int
	Text               string
}

// ReplaceFileGraph atomically replaces all derived rows for fileID.
func (s *Store) ReplaceFileGraph(fileID int64, syms []Symbol, res []Resource, edges []RawEdge, chunks []Chunk) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := deleteFileChildren(tx, fileID); err != nil {
		return err
	}

	nameToID := make(map[string]int64, len(syms))
	for _, sym := range syms {
		r, err := tx.Exec(
			`INSERT INTO symbols(file_id,name,kind,signature,start_line,end_line,complexity) VALUES(?,?,?,?,?,?,?)`,
			fileID, sym.Name, sym.Kind, nullStr(sym.Signature), sym.StartLine, sym.EndLine, sym.Complexity)
		if err != nil {
			return err
		}
		id, _ := r.LastInsertId()
		if _, dup := nameToID[sym.Name]; !dup {
			nameToID[sym.Name] = id
		}
	}
	for _, sym := range syms {
		if sym.ParentName == "" {
			continue
		}
		if pid, ok := nameToID[sym.ParentName]; ok {
			if _, err := tx.Exec(`UPDATE symbols SET parent_id=? WHERE file_id=? AND name=? AND parent_id IS NULL`,
				pid, fileID, sym.Name); err != nil {
				return err
			}
		}
	}
	for _, rsc := range res {
		if _, err := tx.Exec(`INSERT INTO resources(kind,name,value,file_id,line) VALUES(?,?,?,?,?)`,
			rsc.Kind, nullStr(rsc.Name), nullStr(rsc.Value), fileID, rsc.Line); err != nil {
			return err
		}
	}
	for _, e := range edges {
		srcType, srcID := "file", fileID
		if e.SrcName != "" {
			if id, ok := nameToID[e.SrcName]; ok {
				srcType, srcID = "symbol", id
			}
		}
		if _, err := tx.Exec(
			`INSERT INTO edges(src_type,src_id,dst_type,dst_id,kind,file_id,line,resolved,raw) VALUES(?,?,NULL,NULL,?,?,?,0,?)`,
			srcType, srcID, e.Kind, fileID, e.Line, e.Raw); err != nil {
			return err
		}
	}
	for _, c := range chunks {
		if _, err := tx.Exec(`INSERT INTO chunks(file_id,start_line,end_line,text) VALUES(?,?,?,?)`,
			fileID, c.StartLine, c.EndLine, c.Text); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SymbolHit is a search/lookup result for a symbol. Signature and EndLine are
// always emitted (no omitempty) so a mixed result set stays a uniform TOON
// table instead of degrading to the verbose per-item form.
type SymbolHit struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Signature string `json:"signature"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	EndLine   int    `json:"end_line"`
}

// SymbolsByName returns exact-name matches.
func (s *Store) SymbolsByName(name string, limit int) ([]SymbolHit, error) {
	return s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line, sy.end_line
		FROM symbols sy JOIN files f ON f.id=sy.file_id
		WHERE sy.name=? ORDER BY f.rel_path, sy.start_line LIMIT ?`, name, limit)
}

// SearchSymbols runs an FTS5 phrase query over symbol names/signatures.
func (s *Store) SearchSymbols(query string, limit int) ([]SymbolHit, error) {
	return s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line, sy.end_line
		FROM fts_symbols ft JOIN symbols sy ON sy.id=ft.rowid JOIN files f ON f.id=sy.file_id
		WHERE fts_symbols MATCH ? ORDER BY rank, sy.name LIMIT ?`, ftsQuote(query), limit)
}

// SymbolsBySubstring returns symbols whose name contains query (case-
// insensitive). It catches camelCase/snake_case components the FTS tokenizer
// keeps whole, e.g. "cloud" finding "updateCloudClient". Shortest names rank
// first, so the closest match leads. Used as a recall fallback after FTS.
func (s *Store) SymbolsBySubstring(query string, limit int) ([]SymbolHit, error) {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	like := "%" + esc.Replace(query) + "%"
	return s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line, sy.end_line
		FROM symbols sy JOIN files f ON f.id=sy.file_id
		WHERE sy.name LIKE ? ESCAPE '\' ORDER BY length(sy.name), f.rel_path, sy.start_line LIMIT ?`, like, limit)
}

// SymbolSpan is a code definition's line range, used to find the function or
// type that encloses a usage line.
type SymbolSpan struct {
	Name      string
	Kind      string
	StartLine int
	EndLine   int
}

// SymbolSpans returns the definitions in a file (by repo-relative path) ordered
// by start line, so a caller can locate the innermost one enclosing a line.
func (s *Store) SymbolSpans(relPath string) ([]SymbolSpan, error) {
	rows, err := s.db.Query(`
		SELECT sy.name, sy.kind, sy.start_line, sy.end_line
		FROM symbols sy JOIN files f ON f.id=sy.file_id
		WHERE f.rel_path=? ORDER BY sy.start_line`, relPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SymbolSpan
	for rows.Next() {
		var sp SymbolSpan
		if err := rows.Scan(&sp.Name, &sp.Kind, &sp.StartLine, &sp.EndLine); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// SymbolByID returns a single symbol.
func (s *Store) SymbolByID(id int64) (SymbolHit, bool, error) {
	hits, err := s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line, sy.end_line
		FROM symbols sy JOIN files f ON f.id=sy.file_id WHERE sy.id=?`, id)
	if err != nil || len(hits) == 0 {
		return SymbolHit{}, false, err
	}
	return hits[0], true, nil
}

func (s *Store) scanSymbolHits(q string, args ...any) ([]SymbolHit, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SymbolHit
	for rows.Next() {
		var h SymbolHit
		if err := rows.Scan(&h.ID, &h.Name, &h.Kind, &h.Signature, &h.File, &h.Line, &h.EndLine); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ChunkHit is a full-text search result over file content.
type ChunkHit struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	// omitempty so --compact (which clears snippets) drops the column entirely
	// rather than emitting an empty one for every row.
	Snippet string `json:"snippet,omitempty"`
}

// SearchChunks runs an FTS5 query over text chunks, returning highlighted
// snippets. It tries the query as an exact phrase first, then -- only when that
// matches nothing -- falls back to all terms (AND) and finally any term (OR),
// so a natural-language question still returns the most relevant chunks instead
// of nothing when the exact phrase is absent.
func (s *Store) SearchChunks(query string, limit int) ([]ChunkHit, error) {
	matches := []string{ftsQuote(query)}
	if terms := ftsTerms(query); len(terms) > 1 {
		matches = append(matches, strings.Join(terms, " "), strings.Join(terms, " OR "))
	}
	for _, m := range matches {
		hits, err := s.searchChunksMatch(m, limit)
		if err != nil {
			return nil, err
		}
		if len(hits) > 0 {
			return hits, nil
		}
	}
	return nil, nil
}

func (s *Store) searchChunksMatch(match string, limit int) ([]ChunkHit, error) {
	rows, err := s.db.Query(`
		SELECT f.rel_path, c.start_line, c.end_line, snippet(fts_chunks,0,'[',']',' … ',12)
		FROM fts_chunks ft JOIN chunks c ON c.id=ft.rowid JOIN files f ON f.id=c.file_id
		WHERE fts_chunks MATCH ? ORDER BY rank LIMIT ?`, match, limit)
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

// ChunkBody is a matched chunk with its full text and starting line, so a caller
// can pinpoint the exact lines a term appears on (SearchChunks returns only a
// truncated snippet).
type ChunkBody struct {
	File      string
	StartLine int
	Text      string
}

// SearchChunkText runs the same FTS query as SearchChunks but returns each
// matched chunk's full text instead of a snippet.
func (s *Store) SearchChunkText(query string, limit int) ([]ChunkBody, error) {
	matches := []string{ftsQuote(query)}
	if terms := ftsTerms(query); len(terms) > 1 {
		matches = append(matches, strings.Join(terms, " "), strings.Join(terms, " OR "))
	}
	for _, match := range matches {
		hits, err := s.searchChunkTextMatch(match, limit)
		if err != nil {
			return nil, err
		}
		if len(hits) > 0 {
			return hits, nil
		}
	}
	return nil, nil
}

func (s *Store) searchChunkTextMatch(match string, limit int) ([]ChunkBody, error) {
	rows, err := s.db.Query(`
		SELECT f.rel_path, c.start_line, c.text
		FROM fts_chunks ft JOIN chunks c ON c.id=ft.rowid JOIN files f ON f.id=c.file_id
		WHERE fts_chunks MATCH ? ORDER BY rank LIMIT ?`, match, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChunkBody
	for rows.Next() {
		var c ChunkBody
		if err := rows.Scan(&c.File, &c.StartLine, &c.Text); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ftsQuote wraps a user query as a single FTS5 phrase, escaping embedded quotes,
// so arbitrary input cannot trigger FTS query-syntax errors.
func ftsQuote(q string) string {
	return `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
}

// ftsStop are common words dropped from the term-level search fallback so they
// neither over-constrain the AND pass nor dominate the OR pass.
var ftsStop = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "to": true, "in": true,
	"is": true, "for": true, "and": true, "or": true, "on": true, "with": true,
}

// ftsTerms splits a query into individually quoted FTS terms, dropping very
// short tokens and stopwords, for the AND/OR fallback used when the exact
// phrase matches nothing. Each term is quoted so punctuation cannot trigger an
// FTS syntax error.
func ftsTerms(q string) []string {
	var out []string
	for _, f := range strings.Fields(q) {
		if len(f) < 2 || ftsStop[strings.ToLower(f)] {
			continue
		}
		out = append(out, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return out
}
