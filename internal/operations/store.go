// Package operations owns the durable global operational authority.
package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	_ "embed"
	_ "github.com/mattn/go-sqlite3"
)

const schemaVersion = 1

// SchemaIdentity is the on-disk operations schema identity.
const SchemaIdentity = "prowl.operations/v1"

//go:embed migrations/001_principal_sessions_outbox.sql
var migrationV1 string

const (
	EventSchemaVersion    = "prowl.operations.event/v1"
	MaxEventMetadataBytes = 4096
	MaxEventCount         = 1_000_000
)

var (
	ErrClosed            = errors.New("operations store is closed")
	ErrInvalidEvent      = errors.New("invalid operations event")
	ErrInvalidTransition = errors.New("invalid operations transition")
	ErrCursorExpired     = errors.New("operations cursor expired")
)

type Store struct {
	db     *sql.DB
	path   string
	mu     sync.Mutex
	closed bool

	// beforeReplayRows is a package-private deterministic test seam.
	beforeReplayRows func()
}

// Tx is a short-lived operations write transaction. It is valid only inside
// the callback passed to Update.
type Tx struct {
	tx    *sql.Tx
	store *Store
}

func (tx *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.tx.ExecContext(ctx, query, args...)
}

func (tx *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return tx.tx.QueryContext(ctx, query, args...)
}

func (tx *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return tx.tx.QueryRowContext(ctx, query, args...)
}

// ReadTx deliberately exposes no mutation or event-append capability.
type ReadTx struct {
	tx *sql.Tx
}

func (tx *ReadTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return tx.tx.QueryContext(ctx, query, args...)
}

func (tx *ReadTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return tx.tx.QueryRowContext(ctx, query, args...)
}

type StreamState struct {
	StreamScope        string
	ScopeID            string
	Epoch              uint64
	Head               uint64
	RetentionFloor     uint64
	SnapshotURI        string
	PublisherWatermark uint64
}

type EventState string

const (
	EventStateActive    EventState = "active"
	EventStateQueued    EventState = "queued"
	EventStateRunning   EventState = "running"
	EventStateCompleted EventState = "completed"
	EventStateSucceeded EventState = "succeeded"
	EventStateFailed    EventState = "failed"
	EventStateCancelled EventState = "cancelled"
	EventStateBlocked   EventState = "blocked"
	EventStateReview    EventState = "review"
)

// EventMetadata is an allowlisted lifecycle summary. It cannot represent
// prompts, source bodies, credentials, or arbitrary private content.
type EventMetadata struct {
	State         EventState `json:"state,omitempty"`
	MessageCount  uint32     `json:"message_count,omitempty"`
	ToolCallCount uint32     `json:"tool_call_count,omitempty"`
}

func (metadata *EventMetadata) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return ErrInvalidEvent
	}
	for field := range fields {
		switch field {
		case "state", "message_count", "tool_call_count":
		default:
			return ErrInvalidEvent
		}
	}
	type wire EventMetadata
	var value wire
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if !validEventMetadata(EventMetadata(value)) {
		return ErrInvalidEvent
	}
	*metadata = EventMetadata(value)
	return nil
}

type EventInput struct {
	ResourceKind    string
	ResourceID      string
	ResourceVersion uint64
	Kind            string
	ParentEventID   string
	CorrelationID   string
	CausationID     string
	Metadata        EventMetadata
}

type Event struct {
	Sequence             uint64
	ID                   string
	OccurredAt           time.Time
	SchemaVersion        string
	PrincipalID          string
	RequestedProfileID   string
	ResolvedProfileID    string
	SurfaceID            Surface
	DelegatedPrincipalID string
	OwnerPrincipalID     string
	AuthorizationScope   string
	EventInput
}

// DBPath returns the single global operations database path.
func DBPath() (string, error) {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "prowl-agent", "operations.db"), nil
}

func Open(ctx context.Context) (*Store, error) {
	path, err := DBPath()
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
		_ = db.Close()
		return nil, err
	}
	if err = migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='operations_schema')`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return applyMigrationV1(ctx, db)
	}
	var identity string
	var version int
	err := db.QueryRowContext(ctx, `SELECT identity, version FROM operations_schema WHERE id=1`).Scan(&identity, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return applyMigrationV1(ctx, db)
	}
	if err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("operations schema version %d is newer than supported %d", version, schemaVersion)
	}
	if version != schemaVersion {
		return fmt.Errorf("unsupported operations schema version %d", version)
	}
	if identity != SchemaIdentity {
		return fmt.Errorf("operations schema identity %q is not %q", identity, SchemaIdentity)
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

func (s *Store) Path() string { return s.path }

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

func (s *Store) State(ctx context.Context) (StreamState, error) {
	if err := s.checkOpen(); err != nil {
		return StreamState{}, err
	}
	var state StreamState
	err := s.db.QueryRowContext(ctx, `SELECT stream_scope, scope_id, epoch, retention_floor, snapshot_uri, publisher_watermark, (SELECT COALESCE(MAX(sequence),0) FROM outbox) FROM authority WHERE id=1`).Scan(
		&state.StreamScope,
		&state.ScopeID,
		&state.Epoch,
		&state.RetentionFloor,
		&state.SnapshotURI,
		&state.PublisherWatermark,
		&state.Head,
	)
	return state, err
}

// Update executes one immediate write transaction. The callback must not retain
// the transaction after it returns.
func (s *Store) Update(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.begin(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if fn == nil {
		return ErrInvalidTransition
	}
	if err := fn(&Tx{tx: tx, store: s}); err != nil {
		return err
	}
	return tx.Commit()
}

// View executes one consistent read transaction.
func (s *Store) View(ctx context.Context, fn func(*ReadTx) error) error {
	tx, err := s.begin(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if fn == nil {
		return ErrInvalidTransition
	}
	if err := fn(&ReadTx{tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func (tx *Tx) AppendEvent(ctx context.Context, attribution Attribution, input EventInput) (Event, error) {
	if !attribution.validFor(tx.store) {
		return Event{}, ErrInvalidAttribution
	}
	if !validEventInput(input) {
		return Event{}, ErrInvalidEvent
	}
	metadata, err := json.Marshal(input.Metadata)
	if err != nil || len(metadata) > MaxEventMetadataBytes {
		return Event{}, ErrInvalidEvent
	}
	id, err := newID()
	if err != nil {
		return Event{}, err
	}
	occurredAt := time.Now().UTC()
	var scopeID string
	if err := tx.tx.QueryRowContext(ctx, `SELECT scope_id FROM authority WHERE id=1`).Scan(&scopeID); err != nil {
		return Event{}, err
	}
	if scopeID == "" || attribution.ownerPrincipalID != scopeID {
		return Event{}, ErrInvalidAttribution
	}
	result, err := tx.tx.ExecContext(ctx, `INSERT INTO outbox(stream_scope,scope_id,event_id,occurred_at,schema_version,resource_kind,resource_id,resource_version,event_kind,principal_id,requested_profile_id,resolved_profile_id,surface_id,delegated_principal_id,parent_event_id,owner_principal_id,authorization_scope,correlation_id,causation_id,metadata) VALUES('operations',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		scopeID,
		id,
		occurredAt.UnixNano(),
		EventSchemaVersion,
		input.ResourceKind,
		input.ResourceID,
		input.ResourceVersion,
		input.Kind,
		attribution.principalID,
		attribution.requestedProfileID,
		attribution.resolvedProfileID,
		string(attribution.surfaceID),
		nullableString(attribution.delegatedPrincipalID),
		nullableString(input.ParentEventID),
		attribution.ownerPrincipalID,
		attribution.authorizationScope,
		input.CorrelationID,
		input.CausationID,
		metadata,
	)
	if err != nil {
		return Event{}, err
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return Event{}, err
	}
	return Event{
		Sequence:             uint64(sequence),
		ID:                   id,
		OccurredAt:           occurredAt,
		SchemaVersion:        EventSchemaVersion,
		PrincipalID:          attribution.principalID,
		RequestedProfileID:   attribution.requestedProfileID,
		ResolvedProfileID:    attribution.resolvedProfileID,
		SurfaceID:            attribution.surfaceID,
		DelegatedPrincipalID: attribution.delegatedPrincipalID,
		OwnerPrincipalID:     attribution.ownerPrincipalID,
		AuthorizationScope:   attribution.authorizationScope,
		EventInput:           input,
	}, nil
}

func (s *Store) Replay(ctx context.Context, after uint64, limit int) ([]Event, bool, error) {
	if limit <= 0 || limit > 1000 {
		return nil, false, ErrInvalidEvent
	}
	tx, err := s.begin(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var retentionFloor uint64
	if err := tx.QueryRowContext(ctx, `SELECT retention_floor FROM authority WHERE id=1`).Scan(&retentionFloor); err != nil {
		return nil, false, err
	}
	if after < retentionFloor {
		return nil, false, ErrCursorExpired
	}
	if s.beforeReplayRows != nil {
		s.beforeReplayRows()
	}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,event_id,occurred_at,schema_version,resource_kind,resource_id,resource_version,event_kind,principal_id,requested_profile_id,resolved_profile_id,surface_id,COALESCE(delegated_principal_id,''),COALESCE(parent_event_id,''),owner_principal_id,authorization_scope,correlation_id,causation_id,metadata FROM outbox WHERE sequence>? ORDER BY sequence LIMIT ?`, after, limit+1)
	if err != nil {
		return nil, false, err
	}
	events := make([]Event, 0, limit+1)
	for rows.Next() {
		var event Event
		var occurredAt int64
		var surface string
		var metadata []byte
		if err := rows.Scan(
			&event.Sequence,
			&event.ID,
			&occurredAt,
			&event.SchemaVersion,
			&event.ResourceKind,
			&event.ResourceID,
			&event.ResourceVersion,
			&event.Kind,
			&event.PrincipalID,
			&event.RequestedProfileID,
			&event.ResolvedProfileID,
			&surface,
			&event.DelegatedPrincipalID,
			&event.ParentEventID,
			&event.OwnerPrincipalID,
			&event.AuthorizationScope,
			&event.CorrelationID,
			&event.CausationID,
			&metadata,
		); err != nil {
			_ = rows.Close()
			return nil, false, err
		}
		if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
			_ = rows.Close()
			return nil, false, err
		}
		event.SurfaceID = Surface(surface)
		event.OccurredAt = time.Unix(0, occurredAt).UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, false, err
	}
	if err := rows.Close(); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	more := len(events) > limit
	if more {
		events = events[:limit]
	}
	return events, more, nil
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
	tx, err := s.begin(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous, watermark, head uint64
	if err := tx.QueryRowContext(ctx, `SELECT retention_floor,publisher_watermark,(SELECT COALESCE(MAX(sequence),0) FROM outbox) FROM authority WHERE id=1`).Scan(&previous, &watermark, &head); err != nil {
		return err
	}
	if floor < previous ||
		floor > watermark ||
		floor > head ||
		!utf8.ValidString(snapshotURI) ||
		len(snapshotURI) > 1024 ||
		floor > 0 && snapshotURI == "" {
		return ErrInvalidTransition
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE sequence < ?`, floor); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE authority SET retention_floor=?,snapshot_uri=? WHERE id=1`, floor, snapshotURI); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) begin(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	return s.db.BeginTx(ctx, options)
}

func validEventInput(input EventInput) bool {
	return input.ResourceVersion > 0 &&
		validBoundedField(input.ResourceKind, 128) &&
		validBoundedField(input.ResourceID, 256) &&
		validBoundedField(input.Kind, 128) &&
		len(input.CorrelationID) <= 256 &&
		len(input.CausationID) <= 256 &&
		utf8.ValidString(input.CorrelationID) &&
		utf8.ValidString(input.CausationID) &&
		(input.ParentEventID == "" || validID(input.ParentEventID)) &&
		validEventMetadata(input.Metadata)
}

func validEventMetadata(metadata EventMetadata) bool {
	switch metadata.State {
	case "", EventStateActive, EventStateQueued, EventStateRunning, EventStateCompleted,
		EventStateSucceeded, EventStateFailed, EventStateCancelled, EventStateBlocked, EventStateReview:
	default:
		return false
	}
	return metadata.MessageCount <= MaxEventCount && metadata.ToolCallCount <= MaxEventCount
}

func validBoundedField(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) checkOpen() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return nil
}
