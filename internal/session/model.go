// Package session implements the minimal durable resumable session ledger over
// the single B0.1 operations database. It creates a session pinning the exact
// immutable B0.2 profile/prompt/exposure snapshot, appends turn trajectories
// transactionally with their operations outbox event, and reopens after process
// restart returning the frozen pinned bytes without re-resolving mutable state.
//
// The service contracts correspond to the later routes POST /api/v1/sessions,
// POST /api/v1/sessions/{id}/turns, GET /api/v1/sessions/{id}, and
// GET /api/v1/sessions/{id}/exposure without binding domain models to HTTP.
// The authenticated principal, owner, and surface are injected by trusted
// adapters and derived server-side from the operations store; request DTOs
// carry no authoritative actor/principal/owner/delegated fields.
package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Bounds on stored and requested fields. They mirror the operations schema
// column checks so callers fail fast before a transaction begins.
const (
	maxFieldLen       = 256
	maxBodyBytes      = 262144
	maxSnapshotBytes  = 262144
	maxSmallJSONBytes = 16384
	maxEntriesPerTurn = 4096
)

// SessionStatus is the durable lifecycle state of a session.
type SessionStatus string

const (
	SessionActive    SessionStatus = "active"
	SessionCompleted SessionStatus = "completed"
	SessionFailed    SessionStatus = "failed"
	SessionCancelled SessionStatus = "cancelled"
)

// TurnStatus is the durable lifecycle state of one appended turn.
type TurnStatus string

const (
	TurnQueued    TurnStatus = "queued"
	TurnRunning   TurnStatus = "running"
	TurnSucceeded TurnStatus = "succeeded"
	TurnFailed    TurnStatus = "failed"
	TurnCancelled TurnStatus = "cancelled"
)

// EntryKind classifies one ordered item in a turn trajectory.
type EntryKind string

const (
	EntryMessage    EntryKind = "message"
	EntryToolCall   EntryKind = "tool_call"
	EntryToolResult EntryKind = "tool_result"
)

// Sentinel errors returned by the service and repository.
var (
	ErrInvalidRequest  = errors.New("invalid session request")
	ErrSessionNotFound = errors.New("session not found")
	ErrVersionConflict = errors.New("session version conflict")
	ErrSessionTerminal = errors.New("session is terminal")
)

// EntryMetadata is a bounded, allowlisted description of a trajectory entry. It
// cannot carry secret values or arbitrary provider payloads.
type EntryMetadata struct {
	Role       string `json:"role,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// Usage holds usage-ready accounting fields for a turn.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// TurnEntryInput is one ordered message or tool-trajectory item of a turn.
type TurnEntryInput struct {
	Kind     EntryKind     `json:"kind"`
	Body     string        `json:"body"`
	Metadata EntryMetadata `json:"metadata"`
}

// CreateSessionRequest pins the exact immutable B0.2 snapshot and exposure
// canonical bytes at session creation. It contains no authoritative actor,
// principal, owner, surface, or delegated fields: those are injected by the
// trusted adapter and derived server-side from the operations store.
type CreateSessionRequest struct {
	SnapshotBytes   []byte `json:"snapshot"`
	ExposureBytes   []byte `json:"exposure"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
}

// AppendTurnRequest appends one turn trajectory. Optimistic ExpectedVersion and
// IdempotencyKey prevent lost updates and duplicate turns. It carries no
// authoritative actor fields.
type AppendTurnRequest struct {
	SessionID       string           `json:"session_id"`
	IdempotencyKey  string           `json:"idempotency_key"`
	ExpectedVersion int64            `json:"expected_version"`
	RunID           string           `json:"run_id"`
	Status          TurnStatus       `json:"status"`
	Entries         []TurnEntryInput `json:"entries"`
	Usage           Usage            `json:"usage"`
}

// GetSessionRequest addresses one session by identity.
type GetSessionRequest struct {
	SessionID string `json:"session_id"`
}

// EntryView is a persisted trajectory entry.
type EntryView struct {
	ID        string        `json:"id"`
	Ordinal   int64         `json:"ordinal"`
	Kind      EntryKind     `json:"kind"`
	Body      string        `json:"body"`
	Metadata  EntryMetadata `json:"metadata"`
	CreatedAt time.Time     `json:"created_at"`
}

// TurnView is a persisted turn and its ordered entries.
type TurnView struct {
	ID               string      `json:"id"`
	SessionID        string      `json:"session_id"`
	Ordinal          int64       `json:"ordinal"`
	IdempotencyKey   string      `json:"idempotency_key"`
	ExpectedVersion  int64       `json:"expected_version"`
	ResultingVersion int64       `json:"resulting_version"`
	RunID            string      `json:"run_id"`
	Status           TurnStatus  `json:"status"`
	PrincipalID      string      `json:"principal_id"`
	OwnerPrincipalID string      `json:"owner_principal_id"`
	SurfaceID        string      `json:"surface_id"`
	Usage            Usage       `json:"usage"`
	CreatedAt        time.Time   `json:"created_at"`
	CompletedAt      *time.Time  `json:"completed_at,omitempty"`
	Entries          []EntryView `json:"entries"`
}

// SessionView is the durable session ledger projection returned by GetSession.
// SnapshotBytes and ExposureBytes carry the exact pinned canonical B0.2 bytes;
// they are excluded from the JSON wire form (the resume and exposure paths emit
// the raw bytes directly) so the ledger document stays hand-authorable.
type SessionView struct {
	ID                 string        `json:"id"`
	Version            int64         `json:"version"`
	Status             SessionStatus `json:"status"`
	PrincipalID        string        `json:"principal_id"`
	OwnerPrincipalID   string        `json:"owner_principal_id"`
	SurfaceID          string        `json:"surface_id"`
	RequestedProfileID string        `json:"requested_profile_id"`
	ResolvedProfileID  string        `json:"resolved_profile_id"`
	AuthorizationScope string        `json:"authorization_scope"`
	ParentSessionID    string        `json:"parent_session_id,omitempty"`
	SnapshotID         string        `json:"snapshot_id"`
	ExposureID         string        `json:"exposure_id"`
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
	Turns              []TurnView    `json:"turns"`
	SnapshotBytes      []byte        `json:"-"`
	ExposureBytes      []byte        `json:"-"`
}

// ExposureView returns the pinned exposure manifest for the exposure route.
type ExposureView struct {
	SessionID     string `json:"session_id"`
	SnapshotID    string `json:"snapshot_id"`
	ExposureID    string `json:"exposure_id"`
	ExposureBytes []byte `json:"-"`
}

// Service is the session ledger contract. It binds no HTTP or CLI types.
type Service interface {
	CreateSession(ctx context.Context, req CreateSessionRequest) (SessionView, error)
	AppendTurn(ctx context.Context, req AppendTurnRequest) (TurnView, error)
	GetSession(ctx context.Context, req GetSessionRequest) (SessionView, error)
	GetExposure(ctx context.Context, req GetSessionRequest) (ExposureView, error)
}

// DecodeCreateSessionRequest strictly decodes a create request, rejecting any
// unknown field. This is the wire boundary later HTTP adapters reuse: it refuses
// smuggled actor/principal/owner/delegated fields.
func DecodeCreateSessionRequest(data []byte) (CreateSessionRequest, error) {
	return decodeStrict[CreateSessionRequest](data)
}

// DecodeAppendTurnRequest strictly decodes a turn request, rejecting any unknown
// field, including smuggled actor/principal/owner/delegated fields.
func DecodeAppendTurnRequest(data []byte) (AppendTurnRequest, error) {
	return decodeStrict[AppendTurnRequest](data)
}

func decodeStrict[T any](data []byte) (T, error) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if decoder.More() {
		return value, ErrInvalidRequest
	}
	return value, nil
}

func validSessionStatus(status SessionStatus) bool {
	switch status {
	case SessionActive, SessionCompleted, SessionFailed, SessionCancelled:
		return true
	default:
		return false
	}
}

func validTurnStatus(status TurnStatus) bool {
	switch status {
	case TurnQueued, TurnRunning, TurnSucceeded, TurnFailed, TurnCancelled:
		return true
	default:
		return false
	}
}

func terminalSessionStatus(status SessionStatus) bool {
	return status == SessionCompleted || status == SessionFailed || status == SessionCancelled
}

func terminalTurnStatus(status TurnStatus) bool {
	return status == TurnSucceeded || status == TurnFailed || status == TurnCancelled
}

func validEntryKind(kind EntryKind) bool {
	switch kind {
	case EntryMessage, EntryToolCall, EntryToolResult:
		return true
	default:
		return false
	}
}
