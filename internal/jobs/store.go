package jobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "embed"
	_ "github.com/mattn/go-sqlite3"
)

const schemaVersion = 1

//go:embed migrations/001_project_jobs_outbox.sql
var migrationV1 string

const schemaIdentity = "prowl.project-jobs/v1"
const maxEvidenceBytes = 4096
const maxExportArtifactBytes = 256 << 10

type Store struct {
	db      *sql.DB
	path    string
	scopeID string
	mu      sync.Mutex
	closed  bool
	active  map[string]context.CancelFunc
	// beforeEnqueueInsert is a package-private deterministic test seam.
	beforeEnqueueInsert func()
}

// DBPath returns the stable jobs database location and workspace identity.
func DBPath(root string) (string, string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(absolute))
	id := hex.EncodeToString(sum[:])
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "prowl-agent", "projects", id, "jobs.db"), id, nil
}

func Open(ctx context.Context, root string) (*Store, error) {
	path, scopeID, err := DBPath(root)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	if err = os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, path: path, scopeID: scopeID, active: make(map[string]context.CancelFunc)}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	var schemaExists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'jobs_schema')`).Scan(&schemaExists); err != nil {
		return err
	}
	if !schemaExists {
		return applyMigrationV1(ctx, db)
	}
	var version int
	err := db.QueryRowContext(ctx, `SELECT version FROM jobs_schema LIMIT 1`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return applyMigrationV1(ctx, db)
	}
	if err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("jobs schema version %d is newer than supported %d", version, schemaVersion)
	}
	if version != schemaVersion {
		return fmt.Errorf("unsupported jobs schema version %d", version)
	}
	var identity string
	if err := db.QueryRowContext(ctx, `SELECT identity FROM jobs_schema LIMIT 1`).Scan(&identity); err != nil {
		return err
	}
	if identity != schemaIdentity {
		return fmt.Errorf("jobs schema identity %q is not %q", identity, schemaIdentity)
	}
	return nil
}

func applyMigrationV1(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, migrationV1); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Path() string    { return s.path }
func (s *Store) ScopeID() string { return s.scopeID }

// WriteExportArtifact atomically persists a bounded artifact for a running
// export job without exposing the filesystem path outside the jobs boundary.
func (s *Store) WriteExportArtifact(ctx context.Context, id string, content []byte) error {
	if !validID(id) || len(content) == 0 || len(content) > maxExportArtifactBytes {
		return ErrInvalidJob
	}
	if err := s.checkOpen(); err != nil {
		return err
	}
	job, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.Kind != KindExport || job.Status != StatusRunning {
		return ErrInvalidTransition
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := s.exportDirectory()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".export-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		_ = os.Remove(temporary)
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if written, err := file.Write(content); err != nil {
		return err
	} else if written != len(content) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporary, s.exportPath(id)); err != nil {
		return err
	}
	return os.Chmod(s.exportPath(id), 0o600)
}

// ReadExportArtifact returns only completed export artifacts.
func (s *Store) ReadExportArtifact(ctx context.Context, id string) ([]byte, error) {
	if !validID(id) {
		return nil, ErrInvalidJob
	}
	job, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Kind != KindExport || job.Status != StatusSucceeded {
		return nil, ErrInvalidTransition
	}
	content, err := os.ReadFile(s.exportPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrInvalidTransition
	}
	if err != nil {
		return nil, err
	}
	if len(content) == 0 || len(content) > maxExportArtifactBytes {
		return nil, ErrInvalidJob
	}
	return content, nil
}

func (s *Store) exportDirectory() string     { return filepath.Join(filepath.Dir(s.path), "exports") }
func (s *Store) exportPath(id string) string { return filepath.Join(s.exportDirectory(), id+".html") }
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	for _, cancel := range s.active {
		cancel()
	}
	return s.db.Close()
}

func (s *Store) EnqueueOrResumeIndex(ctx context.Context) (Job, bool, error) {
	return s.enqueueOrResume(ctx, KindIndex)
}

func (s *Store) EnqueueOrResumeExport(ctx context.Context) (Job, bool, error) {
	return s.enqueueOrResume(ctx, KindExport)
}

func (s *Store) enqueueOrResume(ctx context.Context, kind Kind) (Job, bool, error) {
	if kind != KindIndex && kind != KindExport {
		return Job{}, false, ErrInvalidJob
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return Job{}, false, err
	}
	defer tx.Rollback()
	job, err := loadActiveKind(ctx, tx, kind)
	if err == nil {
		return job, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, err
	}
	if s.beforeEnqueueInsert != nil {
		s.beforeEnqueueInsert()
	}
	id, err := newID()
	if err != nil {
		return Job{}, false, err
	}
	now := time.Now().UTC()
	job = Job{ID: id, Kind: kind, Status: StatusQueued, Version: 1, Phase: "queued", CreatedAt: now, UpdatedAt: now}
	if err := insertJob(ctx, tx, job); err != nil {
		return Job{}, false, err
	}
	if err := appendChanged(ctx, tx, job); err != nil {
		return Job{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, false, err
	}
	return job, true, nil
}

func (s *Store) Get(ctx context.Context, id string) (Job, error) {
	if id == "" {
		return Job{}, ErrInvalidJob
	}
	if err := s.checkOpen(); err != nil {
		return Job{}, err
	}
	job, err := scanJob(s.db.QueryRowContext(ctx, jobQuery+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrUnknownJob
	}
	return job, err
}

func (s *Store) Cancel(ctx context.Context, id string, expectedVersion uint64) (Job, error) {
	if id == "" || expectedVersion == 0 {
		return Job{}, ErrInvalidJob
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	job, err := scanJob(tx.QueryRowContext(ctx, jobQuery+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrUnknownJob
	}
	if err != nil {
		return Job{}, err
	}
	if job.Version != expectedVersion {
		return Job{}, ErrStaleVersion
	}
	if job.Status == StatusCancelling || job.Status == StatusCancelled {
		return job, tx.Commit()
	}
	if job.Terminal() {
		return Job{}, ErrInvalidTransition
	}
	job.Status, job.Phase, job.Version, job.UpdatedAt = StatusCancelling, "cancelling", job.Version+1, time.Now().UTC()
	if err := updateJob(ctx, tx, job); err != nil {
		return Job{}, err
	}
	if err := appendChanged(ctx, tx, job); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	cancel := s.active[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return job, nil
}

func (s *Store) claim(ctx context.Context) (Job, bool, error) {
	return s.claimRunnable(ctx, false)
}

func (s *Store) claimRunnable(ctx context.Context, includeExport bool) (Job, bool, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return Job{}, false, err
	}
	defer tx.Rollback()
	query := jobQuery + ` WHERE status IN ('queued','cancelling') AND kind='index'`
	if includeExport {
		query = jobQuery + ` WHERE status IN ('queued','cancelling') AND kind IN ('index','export')`
	}
	job, err := scanJob(tx.QueryRowContext(ctx, query+` ORDER BY created_at LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, tx.Commit()
	}
	if err != nil {
		return Job{}, false, err
	}
	if job.Status == StatusCancelling {
		job.Status, job.Phase, job.Outcome = StatusCancelled, "cancelled", "cancelled"
	} else {
		job.Status, job.Phase = StatusRunning, "starting"
	}
	job.Version++
	job.UpdatedAt = time.Now().UTC()
	if err := updateJob(ctx, tx, job); err != nil {
		return Job{}, false, err
	}
	if err := appendChanged(ctx, tx, job); err != nil {
		return Job{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, false, err
	}
	return job, true, nil
}

func (s *Store) reconcile(ctx context.Context) error {
	return s.reconcileRunnable(ctx, false)
}

func (s *Store) reconcileRunnable(ctx context.Context, includeExport bool) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := jobQuery + ` WHERE kind='index' AND status IN ('running','cancelling')`
	if includeExport {
		query = jobQuery + ` WHERE kind IN ('index','export') AND status IN ('running','cancelling')`
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return err
		}
		if job.Status == StatusCancelling {
			job.Status, job.Phase, job.Outcome = StatusCancelled, "cancelled", "cancelled"
		} else {
			job.Status, job.Phase = StatusQueued, "queued"
		}
		job.Version++
		job.UpdatedAt = time.Now().UTC()
		if err := updateJob(ctx, tx, job); err != nil {
			return err
		}
		if err := appendChanged(ctx, tx, job); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) updateProgress(ctx context.Context, id string, phase string, progress int) (Job, error) {
	if progress < 0 || progress > 100 || len(phase) > 128 {
		return Job{}, ErrInvalidJob
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	job, err := scanJob(tx.QueryRowContext(ctx, jobQuery+` WHERE id=?`, id))
	if err != nil {
		return Job{}, err
	}
	if job.Status != StatusRunning {
		return Job{}, ErrInvalidTransition
	}
	if progress < job.Progress {
		return Job{}, ErrInvalidJob
	}
	job.Phase, job.Progress, job.Version, job.UpdatedAt = phase, progress, job.Version+1, time.Now().UTC()
	if err := updateJob(ctx, tx, job); err != nil {
		return Job{}, err
	}
	if err := appendChanged(ctx, tx, job); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return job, nil
}
func (s *Store) finish(ctx context.Context, id string, runErr error) (Job, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	job, err := scanJob(tx.QueryRowContext(ctx, jobQuery+` WHERE id=?`, id))
	if err != nil {
		return Job{}, err
	}
	if job.Status == StatusCancelling || errors.Is(runErr, context.Canceled) {
		job.Status, job.Phase, job.Outcome = StatusCancelled, "cancelled", "cancelled"
	} else if runErr != nil {
		job.Status, job.Phase, job.Outcome, job.ErrorCode = StatusFailed, "failed", "failed", string(job.Kind)+"_failed"
	} else {
		outcome := "succeeded"
		if job.Kind == KindExport {
			outcome = "exported"
		}
		job.Status, job.Phase, job.Progress, job.Outcome = StatusSucceeded, "complete", 100, outcome
	}
	job.Version++
	job.UpdatedAt = time.Now().UTC()
	if err := updateJob(ctx, tx, job); err != nil {
		return Job{}, err
	}
	if err := appendChanged(ctx, tx, job); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	if job.Kind == KindExport && job.Status != StatusSucceeded {
		_ = os.Remove(s.exportPath(job.ID))
	}
	return job, nil
}
func (s *Store) setActive(id string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		cancel()
		return
	}
	s.active[id] = cancel
}
func (s *Store) clearActive(id string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.active, id) }

func (s *Store) State(ctx context.Context) (StreamState, error) {
	if err := s.checkOpen(); err != nil {
		return StreamState{}, err
	}
	var state StreamState
	state.ScopeID = s.scopeID
	err := s.db.QueryRowContext(ctx, `SELECT epoch, retention_floor, snapshot_uri, (SELECT COALESCE(MAX(sequence),0) FROM outbox) FROM authority WHERE id=1`).Scan(&state.Epoch, &state.RetentionFloor, &state.SnapshotURI, &state.Head)
	if err == nil {
		ensureSnapshotURI(&state)
	}
	return state, err
}

// Snapshot reads one job and its durable stream authority from one SQLite read
// transaction, so an API response cannot pair an older job with a newer head.
func (s *Store) Snapshot(ctx context.Context, id string) (Job, StreamState, error) {
	if id == "" {
		return Job{}, StreamState{}, ErrInvalidJob
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return Job{}, StreamState{}, err
	}
	defer tx.Rollback()
	job, err := scanJob(tx.QueryRowContext(ctx, jobQuery+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, StreamState{}, ErrUnknownJob
	}
	if err != nil {
		return Job{}, StreamState{}, err
	}
	state := StreamState{ScopeID: s.scopeID}
	if err := tx.QueryRowContext(ctx, `SELECT epoch, retention_floor, snapshot_uri, (SELECT COALESCE(MAX(sequence),0) FROM outbox) FROM authority WHERE id=1`).Scan(&state.Epoch, &state.RetentionFloor, &state.SnapshotURI, &state.Head); err != nil {
		return Job{}, StreamState{}, err
	}
	ensureSnapshotURI(&state)
	if err := tx.Commit(); err != nil {
		return Job{}, StreamState{}, err
	}
	return job, state, nil
}

func ensureSnapshotURI(state *StreamState) {
	if state != nil && state.SnapshotURI == "" && state.ScopeID != "" {
		state.SnapshotURI = "snapshot://" + state.ScopeID
	}
}
func (s *Store) Replay(ctx context.Context, after uint64, limit int) ([]OutboxRow, bool, error) {
	if limit <= 0 {
		return nil, false, ErrInvalidJob
	}
	state, err := s.State(ctx)
	if err != nil {
		return nil, false, err
	}
	if after < state.RetentionFloor {
		return nil, false, ErrInvalidTransition
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, kind, payload FROM outbox WHERE sequence>? ORDER BY sequence LIMIT ?`, after, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var result []OutboxRow
	for rows.Next() {
		var row OutboxRow
		if err := rows.Scan(&row.Sequence, &row.Kind, &row.Payload); err != nil {
			return nil, false, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(result) > limit
	if more {
		result = result[:limit]
	}
	return result, more, nil
}
func (s *Store) PublisherWatermark(ctx context.Context) (uint64, error) {
	var value uint64
	err := s.db.QueryRowContext(ctx, `SELECT publisher_watermark FROM authority WHERE id=1`).Scan(&value)
	return value, err
}
func (s *Store) SetPublisherWatermark(ctx context.Context, sequence uint64) error {
	if err := s.checkOpen(); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE authority SET publisher_watermark=? WHERE id=1 AND publisher_watermark<=? AND retention_floor<=? AND ?<=(SELECT COALESCE(MAX(sequence),0) FROM outbox)`, sequence, sequence, sequence, sequence)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrInvalidTransition
	}
	return nil
}
func (s *Store) AdvanceRetention(ctx context.Context, floor uint64, snapshotURI string) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous, watermark, head uint64
	if err := tx.QueryRowContext(ctx, `SELECT retention_floor, publisher_watermark FROM authority WHERE id=1`).Scan(&previous, &watermark); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM outbox`).Scan(&head); err != nil {
		return err
	}
	if floor < previous || floor > head {
		return ErrInvalidTransition
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM outbox WHERE sequence < ?`, floor); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE authority SET retention_floor=?, snapshot_uri=?, publisher_watermark=CASE WHEN publisher_watermark < ? THEN ? ELSE publisher_watermark END WHERE id=1`, floor, snapshotURI, floor, floor); err != nil {
		return err
	}
	return tx.Commit()
}

const jobQuery = `SELECT id, kind, status, version, phase, progress, evidence, outcome, error_code, created_at, updated_at FROM jobs`

type rowScanner interface{ Scan(...any) error }

func scanJob(row rowScanner) (Job, error) {
	var job Job
	var created, updated int64
	err := row.Scan(&job.ID, &job.Kind, &job.Status, &job.Version, &job.Phase, &job.Progress, &job.Evidence, &job.Outcome, &job.ErrorCode, &created, &updated)
	job.CreatedAt = time.Unix(0, created).UTC()
	job.UpdatedAt = time.Unix(0, updated).UTC()
	return job, err
}
func loadActiveKind(ctx context.Context, tx *sql.Tx, kind Kind) (Job, error) {
	return scanJob(tx.QueryRowContext(ctx, jobQuery+` WHERE kind=? AND status IN ('queued','running','cancelling') LIMIT 1`, kind))
}
func insertJob(ctx context.Context, tx *sql.Tx, j Job) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO jobs(id,kind,status,version,phase,progress,evidence,outcome,error_code,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, j.ID, j.Kind, j.Status, j.Version, j.Phase, j.Progress, j.Evidence, j.Outcome, j.ErrorCode, j.CreatedAt.UnixNano(), j.UpdatedAt.UnixNano())
	return err
}
func updateJob(ctx context.Context, tx *sql.Tx, j Job) error {
	_, err := tx.ExecContext(ctx, `UPDATE jobs SET status=?,version=?,phase=?,progress=?,evidence=?,outcome=?,error_code=?,updated_at=? WHERE id=?`, j.Status, j.Version, j.Phase, j.Progress, j.Evidence, j.Outcome, j.ErrorCode, j.UpdatedAt.UnixNano(), j.ID)
	return err
}
func appendChanged(ctx context.Context, tx *sql.Tx, j Job) error {
	payload, err := json.Marshal(struct {
		ID        string `json:"job_id"`
		Version   uint64 `json:"version"`
		Status    Status `json:"status"`
		Phase     string `json:"phase"`
		Progress  int    `json:"progress"`
		Outcome   string `json:"outcome,omitempty"`
		ErrorCode string `json:"error_code,omitempty"`
	}{j.ID, j.Version, j.Status, j.Phase, j.Progress, j.Outcome, j.ErrorCode})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox(kind,payload) VALUES('project-job.changed',?)`, payload)
	return err
}
func (s *Store) begin(ctx context.Context) (*sql.Tx, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	return s.db.BeginTx(ctx, nil)
}
func (s *Store) checkOpen() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return nil
}
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}
