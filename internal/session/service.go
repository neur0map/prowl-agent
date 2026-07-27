package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/prowl-agent/prowl-agent/internal/agent"
	"github.com/prowl-agent/prowl-agent/internal/operations"
	"github.com/prowl-agent/prowl-agent/internal/profile"
)

// ledgerService is the durable session ledger over the shared operations store.
type ledgerService struct {
	store   *operations.Store
	surface operations.Surface
	repo    repository
}

// NewService constructs a session service bound to a single operations store and
// the trusted surface of the calling adapter (for example operations.SurfaceCLI).
func NewService(store *operations.Store, surface operations.Surface) Service {
	return &ledgerService{store: store, surface: surface}
}

// CreateSession validates and pins the exact immutable B0.2 snapshot and
// exposure canonical bytes, then commits the session row and its operations
// outbox event in one transaction.
func (s *ledgerService) CreateSession(ctx context.Context, req CreateSessionRequest) (SessionView, error) {
	if len(req.SnapshotBytes) == 0 || len(req.SnapshotBytes) > maxSnapshotBytes {
		return SessionView{}, ErrInvalidRequest
	}
	if len(req.ExposureBytes) == 0 || len(req.ExposureBytes) > maxSnapshotBytes {
		return SessionView{}, ErrInvalidRequest
	}
	if req.ParentSessionID != "" && !validBounded(req.ParentSessionID, maxFieldLen) {
		return SessionView{}, ErrInvalidRequest
	}
	snapshot, err := profile.OpenSnapshot(req.SnapshotBytes)
	if err != nil {
		return SessionView{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	manifest, err := agent.OpenExposureManifest(req.ExposureBytes)
	if err != nil {
		return SessionView{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if manifest.SnapshotID() != snapshot.ID() {
		return SessionView{}, fmt.Errorf("%w: exposure manifest does not pin this snapshot", ErrInvalidRequest)
	}

	attribution, err := s.store.LocalAttribution(ctx, s.surface)
	if err != nil {
		return SessionView{}, err
	}
	id, err := newID()
	if err != nil {
		return SessionView{}, err
	}
	nowNanos := time.Now().UTC().UnixNano()
	row := sessionRow{
		id:                   id,
		version:              1,
		status:               string(SessionActive),
		principalID:          attribution.PrincipalID(),
		requestedProfileID:   attribution.RequestedProfileID(),
		resolvedProfileID:    attribution.ResolvedProfileID(),
		surfaceID:            string(attribution.SurfaceID()),
		delegatedPrincipalID: attribution.DelegatedPrincipalID(),
		parentSessionID:      req.ParentSessionID,
		ownerPrincipalID:     attribution.OwnerPrincipalID(),
		authorizationScope:   attribution.AuthorizationScope(),
		snapshotJSON:         snapshot.CanonicalBytes(),
		exposureJSON:         manifest.CanonicalBytes(),
		createdAt:            nowNanos,
		updatedAt:            nowNanos,
	}
	if err := s.store.Update(ctx, func(tx *operations.Tx) error {
		if err := s.repo.insertSession(ctx, tx, row); err != nil {
			return err
		}
		_, err := tx.AppendEvent(ctx, attribution, operations.EventInput{
			ResourceKind:    "session",
			ResourceID:      id,
			ResourceVersion: 1,
			Kind:            "session.created",
			CorrelationID:   id,
			Metadata:        operations.EventMetadata{State: operations.EventStateActive},
		})
		return err
	}); err != nil {
		return SessionView{}, err
	}
	return sessionViewFromRow(row, snapshot.ID(), manifest.ID(), []TurnView{}), nil
}

// AppendTurn appends one turn trajectory. It is idempotent per (session,
// idempotency key), uses optimistic ExpectedVersion to prevent lost updates, and
// commits every entry, the session version bump, and the outbox event together.
func (s *ledgerService) AppendTurn(ctx context.Context, req AppendTurnRequest) (TurnView, error) {
	if !validBounded(req.SessionID, maxFieldLen) || !validBounded(req.IdempotencyKey, maxFieldLen) || !validBounded(req.RunID, maxFieldLen) {
		return TurnView{}, ErrInvalidRequest
	}
	if req.ExpectedVersion <= 0 || !validTurnStatus(req.Status) || len(req.Entries) > maxEntriesPerTurn {
		return TurnView{}, ErrInvalidRequest
	}
	bodies, metas, messageCount, toolCallCount, err := prepareEntries(req.Entries)
	if err != nil {
		return TurnView{}, err
	}
	usageJSON, err := marshalBounded(req.Usage, maxSmallJSONBytes)
	if err != nil {
		return TurnView{}, err
	}
	attribution, err := s.store.LocalAttribution(ctx, s.surface)
	if err != nil {
		return TurnView{}, err
	}

	var result TurnView
	if err := s.store.Update(ctx, func(tx *operations.Tx) error {
		session, found, err := s.repo.session(ctx, tx, req.SessionID)
		if err != nil {
			return err
		}
		if !found {
			return ErrSessionNotFound
		}
		// Idempotent replay: an existing turn for this key returns unchanged,
		// appending neither a new turn nor a new event.
		if existing, ok, err := s.repo.turnByIdempotency(ctx, tx, req.SessionID, req.IdempotencyKey); err != nil {
			return err
		} else if ok {
			entries, err := s.turnEntries(ctx, tx, req.SessionID, existing.id)
			if err != nil {
				return err
			}
			result, err = turnViewFromRow(existing, entries)
			return err
		}
		if terminalSessionStatus(SessionStatus(session.status)) {
			return ErrSessionTerminal
		}
		if req.ExpectedVersion != session.version {
			return ErrVersionConflict
		}
		newVersion := session.version + 1
		ordinal, err := s.repo.nextTurnOrdinal(ctx, tx, req.SessionID)
		if err != nil {
			return err
		}
		turnID, err := newID()
		if err != nil {
			return err
		}
		nowNanos := time.Now().UTC().UnixNano()
		completedAt := sql.NullInt64{}
		if terminalTurnStatus(req.Status) {
			completedAt = sql.NullInt64{Int64: nowNanos, Valid: true}
		}
		turn := turnRow{
			id:                   turnID,
			sessionID:            req.SessionID,
			ordinal:              ordinal,
			idempotencyKey:       req.IdempotencyKey,
			expectedVersion:      req.ExpectedVersion,
			resultingVersion:     newVersion,
			runID:                req.RunID,
			status:               string(req.Status),
			principalID:          attribution.PrincipalID(),
			surfaceID:            string(attribution.SurfaceID()),
			delegatedPrincipalID: attribution.DelegatedPrincipalID(),
			ownerPrincipalID:     attribution.OwnerPrincipalID(),
			authorizationScope:   attribution.AuthorizationScope(),
			usageJSON:            usageJSON,
			createdAt:            nowNanos,
			completedAt:          completedAt,
		}
		if err := s.repo.insertTurn(ctx, tx, turn); err != nil {
			return err
		}
		baseOrdinal, err := s.repo.nextEntryOrdinal(ctx, tx, req.SessionID)
		if err != nil {
			return err
		}
		entries := make([]EntryView, len(req.Entries))
		for index := range req.Entries {
			entryID, err := newID()
			if err != nil {
				return err
			}
			row := entryRow{
				id:                   entryID,
				sessionID:            req.SessionID,
				turnID:               turnID,
				ordinal:              baseOrdinal + int64(index),
				kind:                 string(req.Entries[index].Kind),
				body:                 bodies[index],
				metadata:             metas[index],
				principalID:          attribution.PrincipalID(),
				surfaceID:            string(attribution.SurfaceID()),
				delegatedPrincipalID: attribution.DelegatedPrincipalID(),
				ownerPrincipalID:     attribution.OwnerPrincipalID(),
				authorizationScope:   attribution.AuthorizationScope(),
				createdAt:            nowNanos,
			}
			if err := s.repo.insertEntry(ctx, tx, row); err != nil {
				return err
			}
			view, err := entryViewFromRow(row)
			if err != nil {
				return err
			}
			entries[index] = view
		}
		applied, err := s.repo.bumpSessionVersion(ctx, tx, req.SessionID, session.version, newVersion, nowNanos)
		if err != nil {
			return err
		}
		if !applied {
			return ErrVersionConflict
		}
		if _, err := tx.AppendEvent(ctx, attribution, operations.EventInput{
			ResourceKind:    "session",
			ResourceID:      req.SessionID,
			ResourceVersion: uint64(newVersion),
			Kind:            "session.turn.appended",
			CorrelationID:   req.SessionID,
			CausationID:     turnID,
			Metadata:        operations.EventMetadata{State: turnEventState(req.Status), MessageCount: messageCount, ToolCallCount: toolCallCount},
		}); err != nil {
			return err
		}
		result, err = turnViewFromRow(turn, entries)
		return err
	}); err != nil {
		return TurnView{}, err
	}
	return result, nil
}

// GetSession returns the durable ledger projection including the pinned snapshot
// bytes. It reopens the frozen bytes without re-resolving mutable state.
func (s *ledgerService) GetSession(ctx context.Context, req GetSessionRequest) (SessionView, error) {
	if !validBounded(req.SessionID, maxFieldLen) {
		return SessionView{}, ErrInvalidRequest
	}
	var view SessionView
	if err := s.store.View(ctx, func(rtx *operations.ReadTx) error {
		row, found, err := s.repo.session(ctx, rtx, req.SessionID)
		if err != nil {
			return err
		}
		if !found {
			return ErrSessionNotFound
		}
		turnRows, err := s.repo.turns(ctx, rtx, req.SessionID)
		if err != nil {
			return err
		}
		grouped, err := s.entriesByTurn(ctx, rtx, req.SessionID)
		if err != nil {
			return err
		}
		turns := make([]TurnView, 0, len(turnRows))
		for _, turnRow := range turnRows {
			entries := grouped[turnRow.id]
			if entries == nil {
				entries = []EntryView{}
			}
			turn, err := turnViewFromRow(turnRow, entries)
			if err != nil {
				return err
			}
			turns = append(turns, turn)
		}
		snapshotID, exposureID, err := pinnedIDs(row)
		if err != nil {
			return err
		}
		view = sessionViewFromRow(row, snapshotID, exposureID, turns)
		return nil
	}); err != nil {
		return SessionView{}, err
	}
	return view, nil
}

// GetExposure returns the pinned exposure manifest bytes for a session.
func (s *ledgerService) GetExposure(ctx context.Context, req GetSessionRequest) (ExposureView, error) {
	if !validBounded(req.SessionID, maxFieldLen) {
		return ExposureView{}, ErrInvalidRequest
	}
	var view ExposureView
	if err := s.store.View(ctx, func(rtx *operations.ReadTx) error {
		row, found, err := s.repo.session(ctx, rtx, req.SessionID)
		if err != nil {
			return err
		}
		if !found {
			return ErrSessionNotFound
		}
		snapshotID, exposureID, err := pinnedIDs(row)
		if err != nil {
			return err
		}
		view = ExposureView{
			SessionID:     row.id,
			SnapshotID:    snapshotID,
			ExposureID:    exposureID,
			ExposureBytes: row.exposureJSON,
		}
		return nil
	}); err != nil {
		return ExposureView{}, err
	}
	return view, nil
}

func (s *ledgerService) turnEntries(ctx context.Context, q queryer, sessionID, turnID string) ([]EntryView, error) {
	grouped, err := s.entriesByTurn(ctx, q, sessionID)
	if err != nil {
		return nil, err
	}
	entries := grouped[turnID]
	if entries == nil {
		entries = []EntryView{}
	}
	return entries, nil
}

func (s *ledgerService) entriesByTurn(ctx context.Context, q queryer, sessionID string) (map[string][]EntryView, error) {
	rows, err := s.repo.entries(ctx, q, sessionID)
	if err != nil {
		return nil, err
	}
	grouped := make(map[string][]EntryView)
	for _, row := range rows {
		view, err := entryViewFromRow(row)
		if err != nil {
			return nil, err
		}
		grouped[row.turnID] = append(grouped[row.turnID], view)
	}
	return grouped, nil
}

func pinnedIDs(row sessionRow) (snapshotID, exposureID string, err error) {
	snapshot, err := profile.OpenSnapshot(row.snapshotJSON)
	if err != nil {
		return "", "", err
	}
	manifest, err := agent.OpenExposureManifest(row.exposureJSON)
	if err != nil {
		return "", "", err
	}
	return snapshot.ID(), manifest.ID(), nil
}

func sessionViewFromRow(row sessionRow, snapshotID, exposureID string, turns []TurnView) SessionView {
	return SessionView{
		ID:                 row.id,
		Version:            row.version,
		Status:             SessionStatus(row.status),
		PrincipalID:        row.principalID,
		OwnerPrincipalID:   row.ownerPrincipalID,
		SurfaceID:          row.surfaceID,
		RequestedProfileID: row.requestedProfileID,
		ResolvedProfileID:  row.resolvedProfileID,
		AuthorizationScope: row.authorizationScope,
		ParentSessionID:    row.parentSessionID,
		SnapshotID:         snapshotID,
		ExposureID:         exposureID,
		CreatedAt:          time.Unix(0, row.createdAt).UTC(),
		UpdatedAt:          time.Unix(0, row.updatedAt).UTC(),
		Turns:              turns,
		SnapshotBytes:      row.snapshotJSON,
		ExposureBytes:      row.exposureJSON,
	}
}

func turnViewFromRow(row turnRow, entries []EntryView) (TurnView, error) {
	var usage Usage
	if len(row.usageJSON) > 0 {
		if err := json.Unmarshal(row.usageJSON, &usage); err != nil {
			return TurnView{}, err
		}
	}
	var completedAt *time.Time
	if row.completedAt.Valid {
		value := time.Unix(0, row.completedAt.Int64).UTC()
		completedAt = &value
	}
	return TurnView{
		ID:               row.id,
		SessionID:        row.sessionID,
		Ordinal:          row.ordinal,
		IdempotencyKey:   row.idempotencyKey,
		ExpectedVersion:  row.expectedVersion,
		ResultingVersion: row.resultingVersion,
		RunID:            row.runID,
		Status:           TurnStatus(row.status),
		PrincipalID:      row.principalID,
		OwnerPrincipalID: row.ownerPrincipalID,
		SurfaceID:        row.surfaceID,
		Usage:            usage,
		CreatedAt:        time.Unix(0, row.createdAt).UTC(),
		CompletedAt:      completedAt,
		Entries:          entries,
	}, nil
}

func entryViewFromRow(row entryRow) (EntryView, error) {
	var metadata EntryMetadata
	if len(row.metadata) > 0 {
		if err := json.Unmarshal(row.metadata, &metadata); err != nil {
			return EntryView{}, err
		}
	}
	return EntryView{
		ID:        row.id,
		Ordinal:   row.ordinal,
		Kind:      EntryKind(row.kind),
		Body:      string(row.body),
		Metadata:  metadata,
		CreatedAt: time.Unix(0, row.createdAt).UTC(),
	}, nil
}

func prepareEntries(entries []TurnEntryInput) (bodies, metadatas [][]byte, messageCount, toolCallCount uint32, err error) {
	bodies = make([][]byte, len(entries))
	metadatas = make([][]byte, len(entries))
	for index, entry := range entries {
		if !validEntryKind(entry.Kind) {
			return nil, nil, 0, 0, ErrInvalidRequest
		}
		if len(entry.Body) > maxBodyBytes || !utf8.ValidString(entry.Body) {
			return nil, nil, 0, 0, ErrInvalidRequest
		}
		if !validOptionalField(entry.Metadata.Role) || !validOptionalField(entry.Metadata.ToolName) || !validOptionalField(entry.Metadata.ToolCallID) {
			return nil, nil, 0, 0, ErrInvalidRequest
		}
		metadata, marshalErr := marshalBounded(entry.Metadata, maxSmallJSONBytes)
		if marshalErr != nil {
			return nil, nil, 0, 0, marshalErr
		}
		bodies[index] = []byte(entry.Body)
		metadatas[index] = metadata
		switch entry.Kind {
		case EntryMessage:
			messageCount++
		case EntryToolCall:
			toolCallCount++
		}
	}
	return bodies, metadatas, messageCount, toolCallCount, nil
}

func marshalBounded(value any, limit int) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if len(data) > limit {
		return nil, ErrInvalidRequest
	}
	return data, nil
}

func turnEventState(status TurnStatus) operations.EventState {
	switch status {
	case TurnQueued:
		return operations.EventStateQueued
	case TurnRunning:
		return operations.EventStateRunning
	case TurnSucceeded:
		return operations.EventStateSucceeded
	case TurnFailed:
		return operations.EventStateFailed
	case TurnCancelled:
		return operations.EventStateCancelled
	default:
		return operations.EventStateActive
	}
}

func validBounded(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value)
}

func validOptionalField(value string) bool {
	return value == "" || (len(value) <= maxFieldLen && utf8.ValidString(value))
}

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
