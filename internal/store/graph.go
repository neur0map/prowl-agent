package store

import "strings"

// Symbol is a definition to insert (parent linked by name within the file).
type Symbol struct {
	Name, Kind, Signature string
	StartLine, EndLine    int
	ParentName            string
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
			`INSERT INTO symbols(file_id,name,kind,signature,start_line,end_line) VALUES(?,?,?,?,?,?)`,
			fileID, sym.Name, sym.Kind, nullStr(sym.Signature), sym.StartLine, sym.EndLine)
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

// SymbolHit is a search/lookup result for a symbol.
type SymbolHit struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Signature string `json:"signature,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
}

// SymbolsByName returns exact-name matches.
func (s *Store) SymbolsByName(name string, limit int) ([]SymbolHit, error) {
	return s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line
		FROM symbols sy JOIN files f ON f.id=sy.file_id
		WHERE sy.name=? ORDER BY f.rel_path, sy.start_line LIMIT ?`, name, limit)
}

// SearchSymbols runs an FTS5 phrase query over symbol names/signatures.
func (s *Store) SearchSymbols(query string, limit int) ([]SymbolHit, error) {
	return s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line
		FROM fts_symbols ft JOIN symbols sy ON sy.id=ft.rowid JOIN files f ON f.id=sy.file_id
		WHERE fts_symbols MATCH ? ORDER BY rank, sy.name LIMIT ?`, ftsQuote(query), limit)
}

// SymbolByID returns a single symbol.
func (s *Store) SymbolByID(id int64) (SymbolHit, bool, error) {
	hits, err := s.scanSymbolHits(`
		SELECT sy.id, sy.name, sy.kind, IFNULL(sy.signature,''), f.rel_path, sy.start_line
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
		if err := rows.Scan(&h.ID, &h.Name, &h.Kind, &h.Signature, &h.File, &h.Line); err != nil {
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
	Snippet   string `json:"snippet"`
}

// SearchChunks runs an FTS5 query over text chunks, returning highlighted snippets.
func (s *Store) SearchChunks(query string, limit int) ([]ChunkHit, error) {
	rows, err := s.db.Query(`
		SELECT f.rel_path, c.start_line, c.end_line, snippet(fts_chunks,0,'[',']',' … ',12)
		FROM fts_chunks ft JOIN chunks c ON c.id=ft.rowid JOIN files f ON f.id=c.file_id
		WHERE fts_chunks MATCH ? ORDER BY rank LIMIT ?`, ftsQuote(query), limit)
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

// ftsQuote wraps a user query as a single FTS5 phrase, escaping embedded quotes,
// so arbitrary input cannot trigger FTS query-syntax errors.
func ftsQuote(q string) string {
	return `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
}
