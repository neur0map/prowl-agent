package session

import (
	"context"
	"database/sql"
	"errors"

	"github.com/prowl-agent/prowl-agent/internal/operations"
)

// queryer is satisfied by both *operations.Tx and *operations.ReadTx, letting
// read helpers run inside either a write or a read transaction.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type sessionRow struct {
	id                   string
	version              int64
	status               string
	principalID          string
	requestedProfileID   string
	resolvedProfileID    string
	surfaceID            string
	delegatedPrincipalID string
	parentSessionID      string
	ownerPrincipalID     string
	authorizationScope   string
	snapshotJSON         []byte
	exposureJSON         []byte
	createdAt            int64
	updatedAt            int64
}

type turnRow struct {
	id                   string
	sessionID            string
	ordinal              int64
	idempotencyKey       string
	expectedVersion      int64
	resultingVersion     int64
	runID                string
	status               string
	principalID          string
	surfaceID            string
	delegatedPrincipalID string
	parentTurnID         string
	ownerPrincipalID     string
	authorizationScope   string
	usageJSON            []byte
	createdAt            int64
	completedAt          sql.NullInt64
}

type entryRow struct {
	id                   string
	sessionID            string
	turnID               string
	ordinal              int64
	kind                 string
	body                 []byte
	metadata             []byte
	principalID          string
	surfaceID            string
	delegatedPrincipalID string
	parentEntryID        string
	ownerPrincipalID     string
	authorizationScope   string
	createdAt            int64
}

// repository owns every SQL statement against the shared B0.1 operations tables.
// It holds no state: all writes flow through the caller's operations write
// transaction so state and outbox events commit atomically.
type repository struct{}

func (repository) insertSession(ctx context.Context, tx *operations.Tx, row sessionRow) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO sessions(session_id,version,status,principal_id,requested_profile_id,resolved_profile_id,surface_id,delegated_principal_id,parent_session_id,owner_principal_id,authorization_scope,snapshot_json,exposure_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.id,
		row.version,
		row.status,
		row.principalID,
		row.requestedProfileID,
		row.resolvedProfileID,
		row.surfaceID,
		nullable(row.delegatedPrincipalID),
		nullable(row.parentSessionID),
		row.ownerPrincipalID,
		row.authorizationScope,
		row.snapshotJSON,
		row.exposureJSON,
		row.createdAt,
		row.updatedAt,
	)
	return err
}

func (repository) session(ctx context.Context, q queryer, id string) (sessionRow, bool, error) {
	var row sessionRow
	err := q.QueryRowContext(ctx, `SELECT session_id,version,status,principal_id,requested_profile_id,resolved_profile_id,surface_id,COALESCE(delegated_principal_id,''),COALESCE(parent_session_id,''),owner_principal_id,authorization_scope,snapshot_json,exposure_json,created_at,updated_at FROM sessions WHERE session_id=?`, id).Scan(
		&row.id,
		&row.version,
		&row.status,
		&row.principalID,
		&row.requestedProfileID,
		&row.resolvedProfileID,
		&row.surfaceID,
		&row.delegatedPrincipalID,
		&row.parentSessionID,
		&row.ownerPrincipalID,
		&row.authorizationScope,
		&row.snapshotJSON,
		&row.exposureJSON,
		&row.createdAt,
		&row.updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return sessionRow{}, false, nil
	}
	if err != nil {
		return sessionRow{}, false, err
	}
	return row, true, nil
}

// bumpSessionVersion applies an optimistic version transition. It returns true
// only when the stored version still equalled from, so concurrent writers
// cannot lose an update.
func (repository) bumpSessionVersion(ctx context.Context, tx *operations.Tx, id string, from, to, updatedAt int64) (bool, error) {
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET version=?,updated_at=? WHERE session_id=? AND version=?`, to, updatedAt, id, from)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (repository) nextTurnOrdinal(ctx context.Context, q queryer, sessionID string) (int64, error) {
	var maxOrdinal int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0) FROM session_turns WHERE session_id=?`, sessionID).Scan(&maxOrdinal); err != nil {
		return 0, err
	}
	return maxOrdinal + 1, nil
}

func (repository) nextEntryOrdinal(ctx context.Context, q queryer, sessionID string) (int64, error) {
	var maxOrdinal int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0) FROM session_entries WHERE session_id=?`, sessionID).Scan(&maxOrdinal); err != nil {
		return 0, err
	}
	return maxOrdinal + 1, nil
}

func (r repository) turnByIdempotency(ctx context.Context, q queryer, sessionID, key string) (turnRow, bool, error) {
	return r.scanTurn(q.QueryRowContext(ctx, turnSelect+` WHERE session_id=? AND idempotency_key=?`, sessionID, key))
}

const turnSelect = `SELECT turn_id,session_id,ordinal,idempotency_key,expected_version,resulting_version,run_id,status,principal_id,surface_id,COALESCE(delegated_principal_id,''),COALESCE(parent_turn_id,''),owner_principal_id,authorization_scope,usage_json,created_at,completed_at FROM session_turns`

type rowScanner interface {
	Scan(...any) error
}

func (repository) scanTurn(scanner rowScanner) (turnRow, bool, error) {
	var row turnRow
	err := scanner.Scan(
		&row.id,
		&row.sessionID,
		&row.ordinal,
		&row.idempotencyKey,
		&row.expectedVersion,
		&row.resultingVersion,
		&row.runID,
		&row.status,
		&row.principalID,
		&row.surfaceID,
		&row.delegatedPrincipalID,
		&row.parentTurnID,
		&row.ownerPrincipalID,
		&row.authorizationScope,
		&row.usageJSON,
		&row.createdAt,
		&row.completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return turnRow{}, false, nil
	}
	if err != nil {
		return turnRow{}, false, err
	}
	return row, true, nil
}

func (r repository) turns(ctx context.Context, q queryer, sessionID string) ([]turnRow, error) {
	rows, err := q.QueryContext(ctx, turnSelect+` WHERE session_id=? ORDER BY ordinal`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var turns []turnRow
	for rows.Next() {
		turn, _, err := r.scanTurn(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	return turns, rows.Err()
}

func (repository) insertTurn(ctx context.Context, tx *operations.Tx, row turnRow) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO session_turns(turn_id,session_id,ordinal,idempotency_key,expected_version,resulting_version,run_id,status,principal_id,surface_id,delegated_principal_id,parent_turn_id,owner_principal_id,authorization_scope,usage_json,created_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.id,
		row.sessionID,
		row.ordinal,
		row.idempotencyKey,
		row.expectedVersion,
		row.resultingVersion,
		row.runID,
		row.status,
		row.principalID,
		row.surfaceID,
		nullable(row.delegatedPrincipalID),
		nullable(row.parentTurnID),
		row.ownerPrincipalID,
		row.authorizationScope,
		row.usageJSON,
		row.createdAt,
		nullableInt(row.completedAt),
	)
	return err
}

func (repository) insertEntry(ctx context.Context, tx *operations.Tx, row entryRow) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO session_entries(entry_id,session_id,turn_id,ordinal,entry_kind,body,metadata,principal_id,surface_id,delegated_principal_id,parent_entry_id,owner_principal_id,authorization_scope,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.id,
		row.sessionID,
		row.turnID,
		row.ordinal,
		row.kind,
		row.body,
		row.metadata,
		row.principalID,
		row.surfaceID,
		nullable(row.delegatedPrincipalID),
		nullable(row.parentEntryID),
		row.ownerPrincipalID,
		row.authorizationScope,
		row.createdAt,
	)
	return err
}

func (repository) entries(ctx context.Context, q queryer, sessionID string) ([]entryRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT entry_id,session_id,turn_id,ordinal,entry_kind,body,metadata,principal_id,surface_id,COALESCE(delegated_principal_id,''),COALESCE(parent_entry_id,''),owner_principal_id,authorization_scope,created_at FROM session_entries WHERE session_id=? ORDER BY ordinal`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []entryRow
	for rows.Next() {
		var row entryRow
		if err := rows.Scan(
			&row.id,
			&row.sessionID,
			&row.turnID,
			&row.ordinal,
			&row.kind,
			&row.body,
			&row.metadata,
			&row.principalID,
			&row.surfaceID,
			&row.delegatedPrincipalID,
			&row.parentEntryID,
			&row.ownerPrincipalID,
			&row.authorizationScope,
			&row.createdAt,
		); err != nil {
			return nil, err
		}
		entries = append(entries, row)
	}
	return entries, rows.Err()
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}
