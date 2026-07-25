package events

import (
	"context"
	"testing"
)

func TestAdapterConformanceCommitRollbackOrderingPayloadAndWatermark(t *testing.T) {
	ctx := context.Background()
	outbox, err := NewMemoryOutbox(MemoryOutboxConfig{
		ScopeID:     "workspace-a",
		Epoch:       4,
		SnapshotURI: "snapshot://workspace-a/4",
	})
	if err != nil {
		t.Fatalf("NewMemoryOutbox() error = %v", err)
	}

	initial, err := outbox.State(ctx)
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	rollback, err := outbox.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := rollback.Append("job.created", []byte("rolled-back")); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := rollback.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	replay, err := outbox.Replay(ctx, initial.Head, 8)
	if err != nil {
		t.Fatalf("Replay() after rollback error = %v", err)
	}
	if replay.Reset != nil || len(replay.Events) != 0 {
		t.Fatalf("Replay() after rollback = %#v, want no reset and no events", replay)
	}

	payload := []byte("original")
	commit, err := outbox.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := commit.Append("job.created", payload); err != nil {
		t.Fatalf("Append(first) error = %v", err)
	}
	if err := commit.Append("job.updated", []byte("second")); err != nil {
		t.Fatalf("Append(second) error = %v", err)
	}
	payload[0] = 'X'
	committed, err := commit.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if got, want := len(committed), 2; got != want {
		t.Fatalf("committed event count = %d, want %d", got, want)
	}
	if got, want := committed[0].Cursor.Sequence, uint64(1); got != want {
		t.Fatalf("first committed sequence = %d, want %d", got, want)
	}
	if got, want := committed[1].Cursor.Sequence, uint64(2); got != want {
		t.Fatalf("second committed sequence = %d, want %d", got, want)
	}
	if got, want := string(committed[0].Payload), "original"; got != want {
		t.Fatalf("committed payload = %q, want %q", got, want)
	}
	committed[0].Payload[0] = 'Y'

	replay, err = outbox.Replay(ctx, initial.Head, 8)
	if err != nil {
		t.Fatalf("Replay() after commit error = %v", err)
	}
	if replay.Reset != nil || len(replay.Events) != 2 {
		t.Fatalf("Replay() after commit = %#v, want two events and no reset", replay)
	}
	if got, want := string(replay.Events[0].Payload), "original"; got != want {
		t.Fatalf("replayed payload = %q, want immutable %q", got, want)
	}
	if got, want := replay.Events[0].Cursor.Sequence, uint64(1); got != want {
		t.Fatalf("replayed first sequence = %d, want %d", got, want)
	}
	if got, want := replay.Events[1].Cursor.Sequence, uint64(2); got != want {
		t.Fatalf("replayed second sequence = %d, want %d", got, want)
	}

	watermark, err := outbox.PublisherWatermark(ctx)
	if err != nil {
		t.Fatalf("PublisherWatermark() error = %v", err)
	}
	if watermark != initial.Head {
		t.Fatalf("initial watermark = %#v, want %#v", watermark, initial.Head)
	}
	if err := outbox.SetPublisherWatermark(ctx, committed[0].Cursor); err != nil {
		t.Fatalf("SetPublisherWatermark() error = %v", err)
	}
	watermark, err = outbox.PublisherWatermark(ctx)
	if err != nil {
		t.Fatalf("PublisherWatermark() after set error = %v", err)
	}
	if watermark != committed[0].Cursor {
		t.Fatalf("watermark = %#v, want %#v", watermark, committed[0].Cursor)
	}
}

func TestRetentionResetForStaleAndWrongStreamCursors(t *testing.T) {
	ctx := context.Background()
	outbox, err := NewMemoryOutbox(MemoryOutboxConfig{
		ScopeID:     "workspace-a",
		Epoch:       8,
		SnapshotURI: "snapshot://workspace-a/8",
	})
	if err != nil {
		t.Fatalf("NewMemoryOutbox() error = %v", err)
	}
	initial, err := outbox.State(ctx)
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	transaction, err := outbox.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := transaction.Append("job.created", []byte("one")); err != nil {
		t.Fatalf("Append(first) error = %v", err)
	}
	if err := transaction.Append("job.updated", []byte("two")); err != nil {
		t.Fatalf("Append(second) error = %v", err)
	}
	committed, err := transaction.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := outbox.AdvanceRetention(ctx, committed[0].Cursor); err != nil {
		t.Fatalf("AdvanceRetention() error = %v", err)
	}
	state, err := outbox.State(ctx)
	if err != nil {
		t.Fatalf("State() after retention error = %v", err)
	}

	for name, after := range map[string]Cursor{
		"stale retention": initial.Head,
		"wrong scope": {
			StreamScope: StreamScope("other"),
			ScopeID:     state.Head.ScopeID,
			Epoch:       state.Head.Epoch,
			Sequence:    state.Head.Sequence,
		},
		"wrong scope ID": {
			StreamScope: state.Head.StreamScope,
			ScopeID:     "workspace-b",
			Epoch:       state.Head.Epoch,
			Sequence:    state.Head.Sequence,
		},
		"wrong epoch": {
			StreamScope: state.Head.StreamScope,
			ScopeID:     state.Head.ScopeID,
			Epoch:       state.Head.Epoch + 1,
			Sequence:    state.Head.Sequence,
		},
	} {
		t.Run(name, func(t *testing.T) {
			replay, err := outbox.Replay(ctx, after, 8)
			if err != nil {
				t.Fatalf("Replay() error = %v", err)
			}
			if replay.Reset == nil {
				t.Fatalf("Replay() = %#v, want reset", replay)
			}
			if replay.Reset.Cursor != state.Head {
				t.Fatalf("reset cursor = %#v, want current %#v", replay.Reset.Cursor, state.Head)
			}
			if got, want := replay.Reset.SnapshotURI, state.SnapshotURI; got != want {
				t.Fatalf("reset snapshot URI = %q, want %q", got, want)
			}
		})
	}
}

func TestAdapterConformanceBoundsLargeReplay(t *testing.T) {
	ctx := context.Background()
	outbox, err := NewMemoryOutbox(MemoryOutboxConfig{ScopeID: "workspace-a", Epoch: 1})
	if err != nil {
		t.Fatalf("NewMemoryOutbox() error = %v", err)
	}
	initial, err := outbox.State(ctx)
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	transaction, err := outbox.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	payload := make([]byte, 128*1024)
	for index := range payload {
		payload[index] = byte(index)
	}
	for index := 0; index < 3; index++ {
		if err := transaction.Append("job.updated", payload); err != nil {
			t.Fatalf("Append(%d) error = %v", index, err)
		}
	}
	if _, err := transaction.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	replay, err := outbox.Replay(ctx, initial.Head, 1)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if got, want := len(replay.Events), 1; got != want {
		t.Fatalf("bounded replay event count = %d, want %d", got, want)
	}
	if !replay.More {
		t.Fatal("bounded replay More = false, want true")
	}
	if got, want := len(replay.Events[0].Payload), len(payload); got != want {
		t.Fatalf("bounded replay payload bytes = %d, want %d", got, want)
	}
}
