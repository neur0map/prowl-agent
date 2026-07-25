package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/prowl-agent/prowl-agent/internal/jobs"
)

// ProjectJobsOutbox adapts the jobs database authority to the C1 outbox contract.
type ProjectJobsOutbox struct {
	store        *jobs.Store
	beforeReplay func()
}

var _ Outbox = (*ProjectJobsOutbox)(nil)

func NewProjectJobsOutbox(store *jobs.Store) *ProjectJobsOutbox {
	return &ProjectJobsOutbox{store: store}
}
func (o *ProjectJobsOutbox) State(ctx context.Context) (StreamState, error) {
	if o == nil || o.store == nil {
		return StreamState{}, ErrNilOutbox
	}
	state, err := o.store.State(ctx)
	if err != nil {
		return StreamState{}, err
	}
	return StreamState{Head: projectJobsCursor(state, state.Head), RetentionFloor: projectJobsCursor(state, state.RetentionFloor), SnapshotURI: state.SnapshotURI}, nil
}
func (o *ProjectJobsOutbox) Replay(ctx context.Context, after Cursor, limit int) (ReplayResult, error) {
	if o == nil || o.store == nil {
		return ReplayResult{}, ErrNilOutbox
	}
	if limit < 1 {
		return ReplayResult{}, fmt.Errorf("%w: limit must be positive", ErrInvalidReplayLimit)
	}
	state, err := o.store.State(ctx)
	if err != nil {
		return ReplayResult{}, err
	}
	head := projectJobsCursor(state, state.Head)
	floor := projectJobsCursor(state, state.RetentionFloor)
	if !sameStream(after, head) || after.Sequence < floor.Sequence || after.Sequence > head.Sequence {
		return ReplayResult{Reset: &Reset{Cursor: head, SnapshotURI: state.SnapshotURI}}, nil
	}
	if o.beforeReplay != nil {
		o.beforeReplay()
	}
	rows, more, err := o.store.Replay(ctx, after.Sequence, limit)
	if err != nil {
		if !errors.Is(err, jobs.ErrInvalidTransition) {
			return ReplayResult{}, err
		}
		current, stateErr := o.store.State(ctx)
		if stateErr != nil {
			return ReplayResult{}, stateErr
		}
		floor := projectJobsCursor(current, current.RetentionFloor)
		if sameStream(after, floor) && after.Sequence < floor.Sequence {
			head := projectJobsCursor(current, current.Head)
			return ReplayResult{Reset: &Reset{Cursor: head, SnapshotURI: current.SnapshotURI}}, nil
		}
		return ReplayResult{}, err
	}
	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, Event{Cursor: projectJobsCursor(state, row.Sequence), Kind: row.Kind, Payload: append([]byte(nil), row.Payload...)})
	}
	return ReplayResult{Events: events, More: more}, nil
}
func (o *ProjectJobsOutbox) PublisherWatermark(ctx context.Context) (Cursor, error) {
	if o == nil || o.store == nil {
		return Cursor{}, ErrNilOutbox
	}
	state, err := o.store.State(ctx)
	if err != nil {
		return Cursor{}, err
	}
	sequence, err := o.store.PublisherWatermark(ctx)
	if err != nil {
		return Cursor{}, err
	}
	return projectJobsCursor(state, sequence), nil
}
func (o *ProjectJobsOutbox) SetPublisherWatermark(ctx context.Context, watermark Cursor) error {
	if o == nil || o.store == nil {
		return ErrNilOutbox
	}
	state, err := o.store.State(ctx)
	if err != nil {
		return err
	}
	head := projectJobsCursor(state, state.Head)
	if !sameStream(watermark, head) {
		return fmt.Errorf("%w: %v", ErrInvalidWatermark, watermark)
	}
	if err := o.store.SetPublisherWatermark(ctx, watermark.Sequence); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWatermark, watermark)
	}
	return nil
}
func projectJobsCursor(state jobs.StreamState, sequence uint64) Cursor {
	return Cursor{StreamScope: ProjectJob, ScopeID: state.ScopeID, Epoch: state.Epoch, Sequence: sequence}
}
