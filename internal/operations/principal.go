package operations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

type PrincipalKind string

type PrincipalSource string

const (
	PrincipalLocalOperator PrincipalKind = "local-operator"
	PrincipalDelegated     PrincipalKind = "delegated"

	PrincipalSourceServerLocal     PrincipalSource = "server-local"
	PrincipalSourceServerDelegated PrincipalSource = "server-delegated"
)

var ErrAuthorityConflict = errors.New("operations authority conflict")

type Principal struct {
	ID        string
	Kind      PrincipalKind
	Source    PrincipalSource
	ParentID  string
	CreatedAt time.Time
}

// ResolveLocalPrincipal returns the one server-generated local operator. The
// principal and operations cursor scope are committed atomically.
func (s *Store) ResolveLocalPrincipal(ctx context.Context) (Principal, error) {
	tx, err := s.begin(ctx, nil)
	if err != nil {
		return Principal{}, err
	}
	defer tx.Rollback()

	principal, err := scanLocalPrincipal(tx.QueryRowContext(ctx, `SELECT principal_id,kind,source,COALESCE(parent_principal_id,''),created_at FROM principals WHERE kind='local-operator' LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		id, idErr := newID()
		if idErr != nil {
			return Principal{}, idErr
		}
		principal = Principal{
			ID:        id,
			Kind:      PrincipalLocalOperator,
			Source:    PrincipalSourceServerLocal,
			CreatedAt: time.Now().UTC(),
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO principals(principal_id,kind,source,parent_principal_id,created_at) VALUES(?,?,?,?,?)`, principal.ID, principal.Kind, principal.Source, nil, principal.CreatedAt.UnixNano()); err != nil {
			return Principal{}, err
		}
	} else if err != nil {
		return Principal{}, err
	}

	snapshotURI := "snapshot://operations/" + principal.ID
	result, err := tx.ExecContext(ctx, `UPDATE authority SET scope_id=?,snapshot_uri=CASE WHEN snapshot_uri='' THEN ? ELSE snapshot_uri END WHERE id=1 AND (scope_id='' OR scope_id=?)`, principal.ID, snapshotURI, principal.ID)
	if err != nil {
		return Principal{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Principal{}, err
	}
	if changed != 1 {
		return Principal{}, ErrAuthorityConflict
	}
	if err := tx.Commit(); err != nil {
		return Principal{}, err
	}
	return principal, nil
}

type principalScanner interface {
	Scan(...any) error
}

func scanLocalPrincipal(row principalScanner) (Principal, error) {
	var principal Principal
	var createdAt int64
	if err := row.Scan(&principal.ID, &principal.Kind, &principal.Source, &principal.ParentID, &createdAt); err != nil {
		return Principal{}, err
	}
	principal.CreatedAt = time.Unix(0, createdAt).UTC()
	return principal, nil
}

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
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
