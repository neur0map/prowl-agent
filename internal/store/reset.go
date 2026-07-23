package store

import "context"

var resetDerivedStatements = []string{
	`DROP TRIGGER IF EXISTS symbols_ad`,
	`DROP TRIGGER IF EXISTS chunks_ad`,
	`DELETE FROM edges`,
	`DELETE FROM resources`,
	`DELETE FROM symbols`,
	`DELETE FROM chunks`,
	`INSERT INTO fts_symbols(fts_symbols) VALUES('delete-all')`,
	`INSERT INTO fts_chunks(fts_chunks) VALUES('delete-all')`,
	`DROP TABLE IF EXISTS vec_chunks`,
	`INSERT INTO meta(key,value) VALUES('index_state','incomplete') ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
	`INSERT INTO meta(key,value) VALUES('vectors_complete','0') ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
}

// ResetDerived clears all derived data for a full re-index. On a populated
// database, per-file deletes are slow: a self-referential foreign key plus the
// FTS delete-sync triggers defeat SQLite's truncate optimization and add per-row
// FTS work (~100k rows on a large rice). Standalone resets pin one connection and
// temporarily disable foreign keys. Generation resets already own a transaction's
// connection, so they perform the same trigger/table work inside that transaction.
func (s *Store) ResetDerived() error {
	ctx := context.Background()
	if s.tx != nil {
		for _, statement := range resetDerivedStatements {
			if _, err := s.tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		_, err := s.tx.ExecContext(ctx, schemaSQL)
		return err
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	for _, statement := range resetDerivedStatements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	_, err = conn.ExecContext(ctx, schemaSQL) // restores foreign_keys=ON and delete triggers
	return err
}
