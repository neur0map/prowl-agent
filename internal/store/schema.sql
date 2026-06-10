PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS files (
  id         INTEGER PRIMARY KEY,
  rel_path   TEXT UNIQUE NOT NULL,
  lang       TEXT NOT NULL,
  role       TEXT,
  size       INTEGER NOT NULL,
  hash       TEXT NOT NULL,
  mtime      INTEGER NOT NULL,
  indexed_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS symbols (
  id         INTEGER PRIMARY KEY,
  file_id    INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL,
  signature  TEXT,
  start_line INTEGER NOT NULL,
  end_line   INTEGER NOT NULL,
  parent_id  INTEGER REFERENCES symbols(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS resources (
  id      INTEGER PRIMARY KEY,
  kind    TEXT NOT NULL,
  name    TEXT,
  value   TEXT,
  file_id INTEGER REFERENCES files(id) ON DELETE CASCADE,
  line    INTEGER
);

CREATE TABLE IF NOT EXISTS edges (
  id       INTEGER PRIMARY KEY,
  src_type TEXT NOT NULL,
  src_id   INTEGER NOT NULL,
  dst_type TEXT,
  dst_id   INTEGER,
  kind     TEXT NOT NULL,
  file_id  INTEGER REFERENCES files(id) ON DELETE CASCADE,
  line     INTEGER,
  resolved INTEGER NOT NULL DEFAULT 0,
  raw      TEXT
);

CREATE TABLE IF NOT EXISTS chunks (
  id         INTEGER PRIMARY KEY,
  file_id    INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  start_line INTEGER NOT NULL,
  end_line   INTEGER NOT NULL,
  text       TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS fts_chunks USING fts5(text, content='chunks', content_rowid='id');
CREATE VIRTUAL TABLE IF NOT EXISTS fts_symbols USING fts5(name, signature, content='symbols', content_rowid='id');

-- Keep external-content FTS indexes in sync with their content tables.
CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
  INSERT INTO fts_chunks(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
  INSERT INTO fts_chunks(fts_chunks, rowid, text) VALUES ('delete', old.id, old.text);
END;
CREATE TRIGGER IF NOT EXISTS symbols_ai AFTER INSERT ON symbols BEGIN
  INSERT INTO fts_symbols(rowid, name, signature) VALUES (new.id, new.name, new.signature);
END;
CREATE TRIGGER IF NOT EXISTS symbols_ad AFTER DELETE ON symbols BEGIN
  INSERT INTO fts_symbols(fts_symbols, rowid, name, signature) VALUES ('delete', old.id, old.name, old.signature);
END;

CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);

CREATE INDEX IF NOT EXISTS idx_edges_dst      ON edges(dst_type, dst_id, kind);
CREATE INDEX IF NOT EXISTS idx_edges_src      ON edges(src_type, src_id, kind);
CREATE INDEX IF NOT EXISTS idx_edges_resolved ON edges(resolved);
CREATE INDEX IF NOT EXISTS idx_symbols_name   ON symbols(name);
CREATE INDEX IF NOT EXISTS idx_symbols_file   ON symbols(file_id);
CREATE INDEX IF NOT EXISTS idx_edges_file     ON edges(file_id);
CREATE INDEX IF NOT EXISTS idx_resources_file ON resources(file_id);
CREATE INDEX IF NOT EXISTS idx_chunks_file    ON chunks(file_id);
CREATE INDEX IF NOT EXISTS idx_resources_name ON resources(name);
CREATE INDEX IF NOT EXISTS idx_files_role     ON files(role);
