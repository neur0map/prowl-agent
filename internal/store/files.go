package store

import (
	"database/sql"
	"time"
)

// File mirrors a row in the files table.
type File struct {
	ID        int64
	RelPath   string
	Lang      string
	Role      string
	Size      int64
	Hash      string
	MTime     int64
	IndexedAt int64
}

// UpsertFile inserts or updates a file by rel_path and returns its id.
func (s *Store) UpsertFile(f File) (int64, error) {
	if f.IndexedAt == 0 {
		f.IndexedAt = time.Now().Unix()
	}
	_, err := s.db.Exec(`
		INSERT INTO files(rel_path,lang,role,size,hash,mtime,indexed_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(rel_path) DO UPDATE SET
			lang=excluded.lang, role=excluded.role, size=excluded.size,
			hash=excluded.hash, mtime=excluded.mtime, indexed_at=excluded.indexed_at`,
		f.RelPath, f.Lang, nullStr(f.Role), f.Size, f.Hash, f.MTime, f.IndexedAt)
	if err != nil {
		return 0, err
	}
	return s.FileID(f.RelPath)
}

// FileID returns the id for a rel_path (sql.ErrNoRows if absent).
func (s *Store) FileID(relPath string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM files WHERE rel_path=?`, relPath).Scan(&id)
	return id, err
}

// GetFileByPath returns the file row and whether it exists.
func (s *Store) GetFileByPath(relPath string) (File, bool, error) {
	f, err := scanFile(s.db.QueryRow(
		`SELECT id,rel_path,lang,IFNULL(role,''),size,hash,mtime,indexed_at FROM files WHERE rel_path=?`, relPath))
	if err == sql.ErrNoRows {
		return File{}, false, nil
	}
	if err != nil {
		return File{}, false, err
	}
	return f, true, nil
}

// AllFiles returns every indexed file ordered by path.
func (s *Store) AllFiles() ([]File, error) {
	rows, err := s.db.Query(`SELECT id,rel_path,lang,IFNULL(role,''),size,hash,mtime,indexed_at FROM files ORDER BY rel_path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DeleteFileByPath removes a file and all its derived rows.
func (s *Store) DeleteFileByPath(relPath string) error {
	id, err := s.FileID(relPath)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return s.deleteFileByID(id)
}

func (s *Store) deleteFileByID(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deleteFileChildren(tx, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// deleteFileChildren removes derived rows for a file via direct deletes so the
// AFTER DELETE FTS-sync triggers fire (cascade deletes are not relied upon).
func deleteFileChildren(tx *sql.Tx, id int64) error {
	if err := deleteChunkVectors(tx, id); err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM chunks WHERE file_id=?`,
		`DELETE FROM symbols WHERE file_id=?`,
		`DELETE FROM resources WHERE file_id=?`,
		`DELETE FROM edges WHERE file_id=?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanFile(sc scanner) (File, error) {
	var f File
	err := sc.Scan(&f.ID, &f.RelPath, &f.Lang, &f.Role, &f.Size, &f.Hash, &f.MTime, &f.IndexedAt)
	return f, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
