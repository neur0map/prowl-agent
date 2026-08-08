package store

import (
	"strconv"
	"strings"
)

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
	return s.writeTransaction(func(tx writeRunner) error {
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
		return nil
	})
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

// DistinctSymbolNamesByLength returns distinct symbol names whose character
// length is within [minLen, maxLen], the candidate set for find's typo-tolerant
// fallback. Bounded so a huge repo cannot make the fuzzy scan unbounded.
func (s *Store) DistinctSymbolNamesByLength(minLen, maxLen int) ([]string, error) {
	if minLen < 1 {
		minLen = 1
	}
	rows, err := s.sql().Query(`SELECT DISTINCT name FROM symbols WHERE length(name) BETWEEN ? AND ? ORDER BY name LIMIT 20000`, minLen, maxLen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
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
	rows, err := s.sql().Query(`
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
	rows, err := s.sql().Query(q, args...)
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
// snippets. It merges three match tiers in precision order -- the exact phrase,
// then all terms (AND), then any term (OR) -- deduping by chunk and capping at
// limit. When the FTS tiers leave slots unfilled it appends a substring
// fallback, so a query for "battery" still recalls chunks that only contain the
// compound "batteryChargeLevel", which the FTS tokenizer keeps whole.
func (s *Store) SearchChunks(query string, limit int) ([]ChunkHit, error) {
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
		hits, err := s.searchChunksMatch(m, limit)
		if err != nil {
			return nil, err
		}
		for _, h := range hits {
			if push(h) {
				return out, nil
			}
		}
	}
	if len(out) < limit {
		terms := searchTerms(query)
		matches, err := s.chunksBySubstring(terms, limit)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if push(ChunkHit{File: m.File, StartLine: m.StartLine, EndLine: m.EndLine, Snippet: substringSnippet(m.Text, terms)}) {
				return out, nil
			}
		}
	}
	return out, nil
}

// ftsMatchTiers returns FTS match strings in precision order: the exact phrase,
// then all terms (AND), then any term (OR). Only multi-term queries add the
// AND/OR tiers.
func ftsMatchTiers(query string) []string {
	matches := []string{ftsQuote(query)}
	if terms := ftsTerms(query); len(terms) > 1 {
		matches = append(matches, strings.Join(terms, " "), strings.Join(terms, " OR "))
	}
	return matches
}

func (s *Store) searchChunksMatch(match string, limit int) ([]ChunkHit, error) {
	rows, err := s.sql().Query(`
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

// SearchChunkText runs the same tiered, merged FTS query as SearchChunks, with
// the same substring fallback, but returns each matched chunk's full text
// instead of a snippet.
func (s *Store) SearchChunkText(query string, limit int) ([]ChunkBody, error) {
	seen := map[string]bool{}
	var out []ChunkBody
	push := func(c ChunkBody) bool {
		key := c.File + "\x00" + strconv.Itoa(c.StartLine)
		if seen[key] {
			return false
		}
		seen[key] = true
		out = append(out, c)
		return len(out) >= limit
	}
	for _, match := range ftsMatchTiers(query) {
		hits, err := s.searchChunkTextMatch(match, limit)
		if err != nil {
			return nil, err
		}
		for _, c := range hits {
			if push(c) {
				return out, nil
			}
		}
	}
	if len(out) < limit {
		matches, err := s.chunksBySubstring(searchTerms(query), limit)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if push(ChunkBody{File: m.File, StartLine: m.StartLine, Text: m.Text}) {
				return out, nil
			}
		}
	}
	return out, nil
}

func (s *Store) searchChunkTextMatch(match string, limit int) ([]ChunkBody, error) {
	rows, err := s.sql().Query(`
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

// chunkMatch is a chunk located by literal substring rather than FTS.
type chunkMatch struct {
	File      string
	StartLine int
	EndLine   int
	Text      string
}

// chunksBySubstring finds chunks whose text contains every term as a literal
// substring (case-insensitive), shortest chunk first. It catches component words
// the FTS tokenizer keeps inside a compound identifier, e.g. "battery" in
// "batteryChargeLevel", mirroring SymbolsBySubstring. instr keeps the match
// literal, so identifier characters like '_' are not treated as wildcards.
func (s *Store) chunksBySubstring(terms []string, limit int) ([]chunkMatch, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	where := make([]string, len(terms))
	args := make([]any, 0, len(terms)+1)
	for i, t := range terms {
		where[i] = "instr(lower(c.text), ?) > 0"
		args = append(args, strings.ToLower(t))
	}
	args = append(args, limit)
	rows, err := s.sql().Query(`
		SELECT f.rel_path, c.start_line, c.end_line, c.text
		FROM chunks c JOIN files f ON f.id=c.file_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY length(c.text) LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chunkMatch
	for rows.Next() {
		var m chunkMatch
		if err := rows.Scan(&m.File, &m.StartLine, &m.EndLine, &m.Text); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// substringSnippet returns a short window of text around the first matching term,
// padded with ellipses, for substring hits that have no FTS match to highlight.
func substringSnippet(text string, terms []string) string {
	low := strings.ToLower(text)
	idx := len(text)
	for _, t := range terms {
		if p := strings.Index(low, strings.ToLower(t)); p >= 0 && p < idx {
			idx = p
		}
	}
	if idx == len(text) {
		idx = 0
	}
	const pad = 60
	start, end := idx-pad, idx+pad
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	snip := strings.TrimSpace(strings.ToValidUTF8(text[start:end], ""))
	if start > 0 {
		snip = " … " + snip
	}
	if end < len(text) {
		snip += " … "
	}
	return snip
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

// searchTerms splits a query into meaningful raw terms, dropping very short
// tokens and stopwords. It underlies both the FTS term tiers and the substring
// fallback, which needs the terms unquoted for a literal instr match.
func searchTerms(q string) []string {
	var out []string
	for _, f := range strings.Fields(q) {
		if len(f) < 2 || ftsStop[strings.ToLower(f)] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// ftsTerms quotes each raw term for the AND/OR fallback used when the exact
// phrase matches nothing, so punctuation cannot trigger an FTS syntax error.
func ftsTerms(q string) []string {
	raw := searchTerms(q)
	out := make([]string, len(raw))
	for i, f := range raw {
		out[i] = `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
	}
	return out
}
