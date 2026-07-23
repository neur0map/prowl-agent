// Package store is the SQLite persistence layer: schema, files/graph writes,
// FTS search, and graph read queries for Prowl Agent's per-folder index.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

// SchemaVersion is bumped whenever Open must migrate an existing cache.
const SchemaVersion = 3

// Store wraps a SQLite connection to a single project's index.db.
type Store struct{ db *sql.DB }

// Open opens (creating if needed) the index database at path, applies the
// schema, and records the schema version. WAL mode lets `index` write while
// the MCP server reads.
func Open(path string) (*Store, error) {
	_, statErr := os.Stat(path)
	existed := statErr == nil
	dsn := "file:" + path + "?_journal_mode=WAL&_foreign_keys=on&_recursive_triggers=on&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	current := currentSchemaVersion(db)
	if current > SchemaVersion {
		db.Close()
		return nil, fmt.Errorf("database schema version %d is newer than supported version %d", current, SchemaVersion)
	}
	if existed && current < SchemaVersion {
		if _, err := backupDatabase(db, path, current); err != nil {
			db.Close()
			return nil, fmt.Errorf("backup schema v%d database: %w", current, err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		db.Close()
		return nil, err
	}
	if _, err := tx.Exec(schemaSQL); err != nil {
		_ = tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// Additive migration for indexes created before the symbols.complexity column
	// existed. CREATE TABLE IF NOT EXISTS above does not alter an existing table,
	// so add the column here; the duplicate-column error on fresh DBs is ignored.
	if _, err := tx.Exec(`ALTER TABLE symbols ADD COLUMN complexity INTEGER NOT NULL DEFAULT 1`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		_ = tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("migrate symbols complexity: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO meta(key,value) VALUES('schema_version',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, SchemaVersion); err != nil {
		_ = tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("set schema_version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		db.Close()
		return nil, fmt.Errorf("commit schema migration: %w", err)
	}
	return &Store{db: db}, nil
}

func currentSchemaVersion(db *sql.DB) int {
	var exists int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='meta'`).Scan(&exists); err != nil || exists == 0 {
		return 0
	}
	var value string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&value); err != nil {
		return 0
	}
	version, _ := strconv.Atoi(value)
	return version
}

func backupDatabase(db *sql.DB, path string, version int) (string, error) {
	base := fmt.Sprintf("%s.bak-v%d", path, version)
	backup := base
	if _, err := os.Stat(backup); err == nil {
		backup = fmt.Sprintf("%s-%d", base, time.Now().UTC().UnixNano())
	}
	// VACUUM INTO captures committed WAL contents and produces a self-contained,
	// verified SQLite file rather than a potentially inconsistent byte copy.
	quoted := strings.ReplaceAll(backup, "'", "''")
	if _, err := db.Exec(`VACUUM INTO '` + quoted + `'`); err != nil {
		return "", err
	}
	return backup, nil
}

// RestoreBackup atomically replaces a closed database with a migration backup.
// Callers must close every Store using dbPath before invoking it.
func RestoreBackup(dbPath, backupPath string) error {
	in, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dbPath + ".restore-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	previous := dbPath + ".restore-previous"
	_ = os.Remove(previous)
	if _, err := os.Stat(dbPath); err == nil {
		if err := os.Rename(dbPath, previous); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dbPath); err != nil {
		_ = os.Rename(previous, dbPath)
		return err
	}
	return nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// SetMeta upserts a key/value into the meta table.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	return err
}

// GetMeta returns the value for key, or "" if absent.
func (s *Store) GetMeta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}
