// Package store is the SQLite persistence layer: schema, files/graph writes,
// FTS search, and graph read queries for Prowl Agent's per-folder index.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

// SchemaVersion is bumped when schema.sql changes in a non-additive way.
const SchemaVersion = 1

// Store wraps a SQLite connection to a single project's index.db.
type Store struct{ db *sql.DB }

// Open opens (creating if needed) the index database at path, applies the
// schema, and records the schema version. WAL mode lets `index` write while
// the MCP server reads.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_journal_mode=WAL&_foreign_keys=on&_recursive_triggers=on&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO meta(key,value) VALUES('schema_version',?)`, SchemaVersion); err != nil {
		db.Close()
		return nil, fmt.Errorf("set schema_version: %w", err)
	}
	return &Store{db: db}, nil
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
