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
  parent_id  INTEGER REFERENCES symbols(id) ON DELETE CASCADE,
  complexity INTEGER NOT NULL DEFAULT 1
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

-- Generic evidence and knowledge cache. Existing code-intelligence tables stay
-- authoritative for their specialized queries during the compatibility period.
CREATE TABLE IF NOT EXISTS artifacts (
  id             TEXT PRIMARY KEY,
  uri            TEXT UNIQUE NOT NULL,
  kind           TEXT NOT NULL,
  title          TEXT,
  mime           TEXT,
  language       TEXT,
  content_hash   TEXT,
  modified_at    TEXT,
  source_adapter TEXT NOT NULL DEFAULT 'local',
  canonical_path TEXT,
  metadata_json  TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS nodes (
  id             TEXT PRIMARY KEY,
  artifact_id    TEXT REFERENCES artifacts(id) ON DELETE CASCADE,
  stable_key     TEXT UNIQUE NOT NULL,
  kind           TEXT NOT NULL,
  name           TEXT NOT NULL,
  summary        TEXT,
  anchor_start   INTEGER,
  anchor_end     INTEGER,
  deterministic INTEGER NOT NULL DEFAULT 1,
  confidence     REAL NOT NULL DEFAULT 1.0,
  valid_from     TEXT,
  valid_to       TEXT,
  metadata_json  TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS relations (
  id             TEXT PRIMARY KEY,
  from_node      TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  to_node        TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  kind           TEXT NOT NULL,
  deterministic INTEGER NOT NULL DEFAULT 1,
  confidence     REAL NOT NULL DEFAULT 1.0,
  evidence_anchor TEXT,
  valid_from     TEXT,
  valid_to       TEXT,
  metadata_json  TEXT NOT NULL DEFAULT '{}',
  UNIQUE(from_node, to_node, kind)
);

CREATE TABLE IF NOT EXISTS knowledge_documents (
  id             TEXT PRIMARY KEY,
  concept_id     TEXT UNIQUE NOT NULL,
  path           TEXT UNIQUE NOT NULL,
  okf_type       TEXT NOT NULL,
  title          TEXT NOT NULL,
  description    TEXT,
  resource       TEXT,
  tags_json      TEXT NOT NULL DEFAULT '[]',
  timestamp      TEXT,
  review_state   TEXT NOT NULL DEFAULT 'confirmed',
  content_hash   TEXT NOT NULL,
  metadata_json  TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS source_anchors (
  id              TEXT PRIMARY KEY,
  knowledge_id    TEXT NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
  uri             TEXT NOT NULL,
  line_start      INTEGER,
  line_end        INTEGER,
  content_hash    TEXT,
  region_hash     TEXT,
  status          TEXT NOT NULL DEFAULT 'current',
  checked_at      TEXT,
  metadata_json   TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS knowledge_proposals (
  id             TEXT PRIMARY KEY,
  operation      TEXT NOT NULL,
  target_path    TEXT NOT NULL,
  proposed_path  TEXT NOT NULL,
  status         TEXT NOT NULL DEFAULT 'proposed',
  author         TEXT,
  created_at     TEXT NOT NULL,
  reviewed_at    TEXT,
  metadata_json  TEXT NOT NULL DEFAULT '{}'
);

CREATE VIEW IF NOT EXISTS compat_artifacts AS
SELECT 'file:' || rel_path AS id,
       'file://' || rel_path AS uri,
       'file' AS kind,
       rel_path AS title,
       lang AS language,
       hash AS content_hash,
       rel_path AS canonical_path
FROM files;

CREATE VIEW IF NOT EXISTS compat_nodes AS
SELECT 'symbol:' || symbols.id AS id,
       'file:' || files.rel_path AS artifact_id,
       files.rel_path || '#L' || symbols.start_line || ':' || symbols.name AS stable_key,
       symbols.kind AS kind,
       symbols.name AS name,
       symbols.signature AS summary,
       symbols.start_line AS anchor_start,
       symbols.end_line AS anchor_end
FROM symbols JOIN files ON files.id = symbols.file_id;

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
CREATE INDEX IF NOT EXISTS idx_artifacts_kind ON artifacts(kind);
CREATE INDEX IF NOT EXISTS idx_nodes_artifact ON nodes(artifact_id);
CREATE INDEX IF NOT EXISTS idx_nodes_kind     ON nodes(kind);
CREATE INDEX IF NOT EXISTS idx_relations_from ON relations(from_node, kind);
CREATE INDEX IF NOT EXISTS idx_relations_to   ON relations(to_node, kind);
CREATE INDEX IF NOT EXISTS idx_knowledge_type ON knowledge_documents(okf_type);
CREATE INDEX IF NOT EXISTS idx_knowledge_state ON knowledge_documents(review_state);
CREATE INDEX IF NOT EXISTS idx_anchor_knowledge ON source_anchors(knowledge_id);
CREATE INDEX IF NOT EXISTS idx_anchor_status ON source_anchors(status);
CREATE INDEX IF NOT EXISTS idx_proposal_status ON knowledge_proposals(status);
