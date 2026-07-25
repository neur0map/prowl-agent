// Package jobs owns durable per-workspace background job state.
package jobs

import (
	"errors"
	"time"
)

var (
	ErrInvalidJob        = errors.New("invalid job")
	ErrUnknownJob        = errors.New("unknown job")
	ErrStaleVersion      = errors.New("stale job version")
	ErrInvalidTransition = errors.New("invalid job transition")
	ErrClosed            = errors.New("jobs store is closed")
)

type Kind string

const (
	KindIndex    Kind = "index"
	KindExport   Kind = "export"
	KindResearch Kind = "research"
	KindSetup    Kind = "setup"
)

type Status string

const (
	StatusQueued     Status = "queued"
	StatusRunning    Status = "running"
	StatusCancelling Status = "cancelling"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
)

type Job struct {
	ID        string
	Kind      Kind
	Status    Status
	Version   uint64
	Phase     string
	Progress  int
	Evidence  string
	Outcome   string
	ErrorCode string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (job Job) Terminal() bool {
	return job.Status == StatusSucceeded || job.Status == StatusFailed || job.Status == StatusCancelled
}

type OutboxRow struct {
	Sequence uint64
	Kind     string
	Payload  []byte
}

type StreamState struct {
	ScopeID                     string
	Epoch, Head, RetentionFloor uint64
	SnapshotURI                 string
}
