package store

import (
	"context"
	"database/sql"
	"path"
	"path/filepath"
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

// ModuleImportLang reports whether a language's import targets are module
// specifiers resolved by a toolchain (Go packages, npm/TS packages, Rust
// crates, Python/Java/C# modules, Ruby gems) rather than project file paths. An
// unresolved import in these languages is an external dependency, not a broken
// reference, so health checks must not flag it as dangling.
func ModuleImportLang(lang string) bool {
	switch lang {
	case "go", "rust", "typescript", "tsx", "javascript", "python", "java", "ruby", "csharp", "php", "kotlin", "dart":
		return true
	}
	return false
}

// UpsertFile inserts or updates a file by rel_path and returns its id.
func (s *Store) UpsertFile(f File) (int64, error) {
	if f.IndexedAt == 0 {
		f.IndexedAt = time.Now().Unix()
	}
	_, err := s.sql().Exec(`
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

// normalizeRel cleans a repo-relative path so an agent-supplied "./pkg/file.go"
// or "pkg/../pkg/file.go" resolves to the stored, cleaned "pkg/file.go".
func normalizeRel(p string) string {
	return path.Clean(filepath.ToSlash(p))
}

// FileID returns the id for a rel_path (sql.ErrNoRows if absent). The path is
// normalized first, so a leading "./" or other slashy noise still resolves.
func (s *Store) FileID(relPath string) (int64, error) {
	var id int64
	err := s.sql().QueryRow(`SELECT id FROM files WHERE rel_path=?`, normalizeRel(relPath)).Scan(&id)
	return id, err
}

// GetFileByPath returns the file row and whether it exists.
func (s *Store) GetFileByPath(relPath string) (File, bool, error) {
	f, err := scanFile(s.sql().QueryRow(
		`SELECT id,rel_path,lang,IFNULL(role,''),size,hash,mtime,indexed_at FROM files WHERE rel_path=?`, normalizeRel(relPath)))
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
	return s.AllFilesContext(context.Background())
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
	return s.writeTransaction(func(tx writeRunner) error {
		if err := deleteFileChildren(tx, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM files WHERE id=?`, id)
		return err
	})
}

// deleteFileChildren removes derived rows for a file via direct deletes so the
// AFTER DELETE FTS-sync triggers fire (cascade deletes are not relied upon).
func deleteFileChildren(tx writeRunner, id int64) error {
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
