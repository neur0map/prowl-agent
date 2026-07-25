package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

const MaxEventKindBytes = 128

var (
	ErrNilContext       = errors.New("nil context")
	ErrNilOutbox        = errors.New("nil outbox")
	ErrInvalidEvent     = errors.New("invalid outbox event")
	ErrInvalidWatermark = errors.New("invalid publisher watermark")
	ErrInvalidRetention = errors.New("invalid retention floor")
	ErrTransactionDone  = errors.New("outbox transaction is already complete")
	ErrInvalidReplayLimit = errors.New("invalid replay limit")
)

type Event struct {
	Cursor  Cursor
	Kind    string
	Payload []byte
}

func (event Event) Validate() error {
	if err := event.Cursor.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidEvent, err)
	}
	if len(event.Kind) == 0 || len(event.Kind) > MaxEventKindBytes {
		return fmt.Errorf("%w: kind must contain 1 through %d bytes", ErrInvalidEvent, MaxEventKindBytes)
	}
	return nil
}

type StreamState struct {
	Head           Cursor
	RetentionFloor Cursor
	SnapshotURI    string
}

type Reset struct {
	Cursor      Cursor
	SnapshotURI string
}

type ReplayResult struct {
	Events []Event
	Reset  *Reset
	More   bool
}

type Outbox interface {
	State(context.Context) (StreamState, error)
	Replay(context.Context, Cursor, int) (ReplayResult, error)
	PublisherWatermark(context.Context) (Cursor, error)
	SetPublisherWatermark(context.Context, Cursor) error
}

type MemoryOutboxConfig struct {
	ScopeID     string
	Epoch       uint64
	SnapshotURI string
}

type MemoryOutbox struct {
	mu        sync.RWMutex
	state     StreamState
	events    []Event
	watermark Cursor
}

var _ Outbox = (*MemoryOutbox)(nil)

func NewMemoryOutbox(config MemoryOutboxConfig) (*MemoryOutbox, error) {
	initial := Cursor{
		StreamScope: ProjectJob,
		ScopeID:     config.ScopeID,
		Epoch:       config.Epoch,
	}
	if err := initial.Validate(); err != nil {
		return nil, err
	}
	return &MemoryOutbox{
		state: StreamState{
			Head:           initial,
			RetentionFloor: initial,
			SnapshotURI:    config.SnapshotURI,
		},
		watermark: initial,
	}, nil
}

func (outbox *MemoryOutbox) State(ctx context.Context) (StreamState, error) {
	if err := contextError(ctx); err != nil {
		return StreamState{}, err
	}
	if outbox == nil {
		return StreamState{}, ErrNilOutbox
	}
	outbox.mu.RLock()
	state := outbox.state
	outbox.mu.RUnlock()
	return state, nil
}

func (outbox *MemoryOutbox) Replay(ctx context.Context, after Cursor, limit int) (ReplayResult, error) {
	if err := contextError(ctx); err != nil {
		return ReplayResult{}, err
	}
	if outbox == nil {
		return ReplayResult{}, ErrNilOutbox
	}
	if limit < 1 {
		return ReplayResult{}, fmt.Errorf("%w: limit must be positive", ErrInvalidReplayLimit)
	}
	outbox.mu.RLock()
	defer outbox.mu.RUnlock()

	if !sameStream(after, outbox.state.Head) || after.Sequence < outbox.state.RetentionFloor.Sequence || after.Sequence > outbox.state.Head.Sequence {
		return ReplayResult{Reset: &Reset{
			Cursor:      outbox.state.Head,
			SnapshotURI: outbox.state.SnapshotURI,
		}}, nil
	}

	capacity := limit
	if len(outbox.events) < capacity {
		capacity = len(outbox.events)
	}
	events := make([]Event, 0, capacity)
	for _, event := range outbox.events {
		if event.Cursor.Sequence <= after.Sequence {
			continue
		}
		if len(events) == limit {
			return ReplayResult{Events: events, More: true}, nil
		}
		events = append(events, cloneEvent(event))
	}
	return ReplayResult{Events: events}, nil
}

func (outbox *MemoryOutbox) PublisherWatermark(ctx context.Context) (Cursor, error) {
	if err := contextError(ctx); err != nil {
		return Cursor{}, err
	}
	if outbox == nil {
		return Cursor{}, ErrNilOutbox
	}
	outbox.mu.RLock()
	watermark := outbox.watermark
	outbox.mu.RUnlock()
	return watermark, nil
}

func (outbox *MemoryOutbox) SetPublisherWatermark(ctx context.Context, watermark Cursor) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if outbox == nil {
		return ErrNilOutbox
	}
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	if !sameStream(watermark, outbox.state.Head) || watermark.Sequence > outbox.state.Head.Sequence || watermark.Sequence < outbox.watermark.Sequence {
		return fmt.Errorf("%w: %v", ErrInvalidWatermark, watermark)
	}
	outbox.watermark = watermark
	return nil
}

func (outbox *MemoryOutbox) Begin(ctx context.Context) (*MemoryTransaction, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if outbox == nil {
		return nil, ErrNilOutbox
	}
	return &MemoryTransaction{outbox: outbox}, nil
}

func (outbox *MemoryOutbox) AdvanceRetention(ctx context.Context, floor Cursor) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if outbox == nil {
		return ErrNilOutbox
	}
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	if !sameStream(floor, outbox.state.Head) || floor.Sequence < outbox.state.RetentionFloor.Sequence || floor.Sequence > outbox.state.Head.Sequence {
		return fmt.Errorf("%w: %v", ErrInvalidRetention, floor)
	}
	outbox.state.RetentionFloor = floor
	retained := make([]Event, 0, len(outbox.events))
	for _, event := range outbox.events {
		if event.Cursor.Sequence > floor.Sequence {
			retained = append(retained, event)
		}
	}
	outbox.events = retained
	return nil
}

type MemoryTransaction struct {
	mu      sync.Mutex
	outbox  *MemoryOutbox
	pending []pendingEvent
	done    bool
}

type pendingEvent struct {
	kind    string
	payload []byte
}

func (transaction *MemoryTransaction) Append(kind string, payload []byte) error {
	if transaction == nil || transaction.outbox == nil {
		return ErrNilOutbox
	}
	if err := validateKind(kind); err != nil {
		return err
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.done {
		return ErrTransactionDone
	}
	transaction.pending = append(transaction.pending, pendingEvent{kind: kind, payload: cloneBytes(payload)})
	return nil
}

func (transaction *MemoryTransaction) Commit(ctx context.Context) ([]Event, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if transaction == nil || transaction.outbox == nil {
		return nil, ErrNilOutbox
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.done {
		return nil, ErrTransactionDone
	}

	outbox := transaction.outbox
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	committed := make([]Event, len(transaction.pending))
	for index, pending := range transaction.pending {
		outbox.state.Head.Sequence++
		event := Event{
			Cursor:  outbox.state.Head,
			Kind:    pending.kind,
			Payload: cloneBytes(pending.payload),
		}
		outbox.events = append(outbox.events, event)
		committed[index] = cloneEvent(event)
	}
	transaction.done = true
	transaction.pending = nil
	return committed, nil
}

func (transaction *MemoryTransaction) Rollback() error {
	if transaction == nil || transaction.outbox == nil {
		return ErrNilOutbox
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.done {
		return ErrTransactionDone
	}
	transaction.done = true
	transaction.pending = nil
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	return ctx.Err()
}

func validateKind(kind string) error {
	if len(kind) == 0 || len(kind) > MaxEventKindBytes {
		return fmt.Errorf("%w: kind must contain 1 through %d bytes", ErrInvalidEvent, MaxEventKindBytes)
	}
	return nil
}

func cloneEvent(event Event) Event {
	event.Payload = cloneBytes(event.Payload)
	return event
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return append([]byte(nil), value...)
}
