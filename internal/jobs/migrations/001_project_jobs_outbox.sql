-- prowl.project-jobs/v1
CREATE TABLE IF NOT EXISTS jobs_schema (identity TEXT NOT NULL, version INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS jobs (id TEXT PRIMARY KEY, kind TEXT NOT NULL, status TEXT NOT NULL, version INTEGER NOT NULL, phase TEXT NOT NULL, progress INTEGER NOT NULL CHECK (progress BETWEEN 0 AND 100), evidence TEXT NOT NULL, outcome TEXT NOT NULL, error_code TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS one_active_index_job ON jobs(kind) WHERE kind = 'index' AND status IN ('queued','running','cancelling');
CREATE TABLE IF NOT EXISTS outbox (sequence INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, payload BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS authority (id INTEGER PRIMARY KEY CHECK (id=1), epoch INTEGER NOT NULL, retention_floor INTEGER NOT NULL, snapshot_uri TEXT NOT NULL, publisher_watermark INTEGER NOT NULL);
INSERT OR IGNORE INTO authority(id, epoch, retention_floor, snapshot_uri, publisher_watermark) VALUES(1, 1, 0, '', 0);
INSERT INTO jobs_schema(identity, version) VALUES('prowl.project-jobs/v1', 1);
