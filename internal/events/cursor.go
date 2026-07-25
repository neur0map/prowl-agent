package events

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidCursor        = errors.New("invalid event cursor")
	ErrCursorStreamMismatch = errors.New("event cursors belong to different streams")
)

type StreamScope string

const ProjectJob StreamScope = "project-job"

type Cursor struct {
	StreamScope StreamScope `json:"stream_scope"`
	ScopeID     string      `json:"scope_id"`
	Epoch       uint64      `json:"epoch"`
	Sequence    uint64      `json:"sequence"`
}

func (cursor Cursor) Validate() error {
	if cursor.StreamScope == "" {
		return fmt.Errorf("%w: stream scope is required", ErrInvalidCursor)
	}
	if cursor.StreamScope != ProjectJob {
		return fmt.Errorf("%w: unsupported stream scope %q", ErrInvalidCursor, cursor.StreamScope)
	}
	if cursor.ScopeID == "" {
		return fmt.Errorf("%w: scope ID is required", ErrInvalidCursor)
	}
	if cursor.Epoch == 0 {
		return fmt.Errorf("%w: epoch must be positive", ErrInvalidCursor)
	}
	return nil
}

func (cursor Cursor) Compare(other Cursor) (int, error) {
	if err := cursor.Validate(); err != nil {
		return 0, err
	}
	if !sameStream(cursor, other) {
		return 0, ErrCursorStreamMismatch
	}
	if err := other.Validate(); err != nil {
		return 0, err
	}
	switch {
	case cursor.Sequence < other.Sequence:
		return -1, nil
	case cursor.Sequence > other.Sequence:
		return 1, nil
	default:
		return 0, nil
	}
}

func sameStream(left, right Cursor) bool {
	return left.StreamScope == right.StreamScope && left.ScopeID == right.ScopeID && left.Epoch == right.Epoch
}
