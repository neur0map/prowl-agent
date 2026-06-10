package store

import "context"

// ResetDerived clears all derived data for a full re-index. On a populated
// database, per-file deletes are slow: a self-referential foreign key plus the
// FTS delete-sync triggers defeat SQLite's truncate optimization and add per-row
// FTS work (~100k rows on a large rice). This pins one connection, disables
// foreign keys, and drops the delete-triggers so the bulk deletes truncate fast,
// clears the FTS and vector indexes directly, then re-applies the schema to
// restore foreign keys and the triggers. Insert triggers are untouched, so the
// re-index that follows repopulates FTS.
func (s *Store) ResetDerived() error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	for _, q := range []string{
		`PRAGMA foreign_keys=OFF`,
		`DROP TRIGGER IF EXISTS symbols_ad`,
		`DROP TRIGGER IF EXISTS chunks_ad`,
		`DELETE FROM edges`,
		`DELETE FROM resources`,
		`DELETE FROM symbols`,
		`DELETE FROM chunks`,
		`INSERT INTO fts_symbols(fts_symbols) VALUES('delete-all')`,
		`INSERT INTO fts_chunks(fts_chunks) VALUES('delete-all')`,
		`DROP TABLE IF EXISTS vec_chunks`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	_, err = conn.ExecContext(ctx, schemaSQL) // restores foreign_keys=ON and the delete triggers
	return err
}
