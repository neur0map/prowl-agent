package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestOpenMigrate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "i.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.GetMeta("schema_version")
	if err != nil {
		t.Fatal(err)
	}
	if v != "2" {
		t.Fatalf("schema_version=%q want 2", v)
	}
	if err := s.SetMeta("x", "y"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.GetMeta("x"); v != "y" {
		t.Fatalf("meta x=%q want y", v)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Re-open must be idempotent.
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	s2.Close()
}

func TestOpenBacksUpAndMigratesVersionOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO meta(key,value) VALUES('schema_version','1'),('fixture','preserved');
		CREATE TABLE files (
			id INTEGER PRIMARY KEY,
			rel_path TEXT UNIQUE NOT NULL,
			lang TEXT NOT NULL,
			role TEXT,
			size INTEGER NOT NULL,
			hash TEXT NOT NULL,
			mtime INTEGER NOT NULL,
			indexed_at INTEGER NOT NULL
		);
		INSERT INTO files(id,rel_path,lang,role,size,hash,mtime,indexed_at)
		VALUES(7,'kept.go','go','source',12,'abc',123,123);
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if version, _ := s.GetMeta("schema_version"); version != "2" {
		t.Fatalf("version after migration = %q", version)
	}
	if fixture, _ := s.GetMeta("fixture"); fixture != "preserved" {
		t.Fatalf("fixture metadata lost: %q", fixture)
	}
	var keptPath string
	if err := s.db.QueryRow(`SELECT rel_path FROM files WHERE id=7`).Scan(&keptPath); err != nil || keptPath != "kept.go" {
		t.Fatalf("v1 file row not preserved: path=%q err=%v", keptPath, err)
	}
	for _, table := range []string{"artifacts", "nodes", "relations", "knowledge_documents", "source_anchors", "knowledge_proposals"} {
		var name string
		if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("missing migrated table %s: %v", table, err)
		}
	}
	backups, err := filepath.Glob(path + ".bak-v1*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("migration backups = %v, %v", backups, err)
	}
	if info, err := os.Stat(backups[0]); err != nil || info.Size() == 0 {
		t.Fatalf("backup is missing or empty: %v", err)
	}
}

func TestOpenRefusesFutureSchemaWithoutModification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT); INSERT INTO meta VALUES('schema_version','99')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("future schema should be refused")
	}
	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&version); err != nil || version != "99" {
		t.Fatalf("future database was modified: version=%q err=%v", version, err)
	}
	if backups, _ := filepath.Glob(path + ".bak-*"); len(backups) != 0 {
		t.Fatalf("future database unexpectedly backed up or modified: %v", backups)
	}
}

func TestSearchChunksPhraseToTermsFallback(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mk := func(rel, text string) {
		fid, err := s.UpsertFile(File{RelPath: rel, Lang: "go", Hash: rel, Size: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceFileGraph(fid, nil, nil, nil, []Chunk{{StartLine: 1, EndLine: 1, Text: text}}); err != nil {
			t.Fatal(err)
		}
	}
	mk("a.go", "draw the status panel and render it inline")
	mk("b.go", "an unrelated gamma helper")

	// Exact phrase present -> the phrase tier matches a.go.
	if hits, err := s.SearchChunks("status panel", 10); err != nil || len(hits) != 1 || hits[0].File != "a.go" {
		t.Fatalf("phrase search = %v, %v; want 1 hit in a.go", hits, err)
	}
	// Phrase absent but all terms co-occur -> the AND tier matches a.go.
	if hits, err := s.SearchChunks("render status panel", 10); err != nil || len(hits) != 1 || hits[0].File != "a.go" {
		t.Fatalf("AND fallback = %v, %v; want 1 hit in a.go", hits, err)
	}
	// No chunk has both terms -> the OR tier returns each chunk matching either.
	if hits, err := s.SearchChunks("panel gamma", 10); err != nil || len(hits) != 2 {
		t.Fatalf("OR fallback = %v, %v; want both files", hits, err)
	}
	// A genuinely absent token yields empty with no error.
	if hits, err := s.SearchChunks("nonexistenttoken", 10); err != nil || len(hits) != 0 {
		t.Fatalf("absent term = %v, %v; want empty", hits, err)
	}
}
