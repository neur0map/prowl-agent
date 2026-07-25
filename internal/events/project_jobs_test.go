package events

import (
	"context"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/jobs"
)

func TestProjectJobsOutboxReplaysBoundedDurableEvents(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := jobs.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	outbox := NewProjectJobsOutbox(store)
	first, _, err := store.EnqueueOrResumeIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Cancel(context.Background(), first.ID, first.Version); err != nil {
		t.Fatal(err)
	}
	state, err := outbox.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := outbox.Replay(context.Background(), Cursor{StreamScope: ProjectJob, ScopeID: state.Head.ScopeID, Epoch: state.Head.Epoch}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || !result.More {
		t.Fatalf("result=%+v", result)
	}
	if result.Events[0].Kind != "project-job.changed" {
		t.Fatalf("event=%+v", result.Events[0])
	}
	wrong := Cursor{StreamScope: ProjectJob, ScopeID: "other", Epoch: state.Head.Epoch}
	result, err = outbox.Replay(context.Background(), wrong, 1)
	if err != nil || result.Reset == nil {
		t.Fatalf("cross-scope result=%+v err=%v", result, err)
	}
}

func TestProjectJobsOutboxResetsWhenRetentionAdvancesDuringReplay(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := jobs.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, _, err := store.EnqueueOrResumeIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Cancel(context.Background(), first.ID, first.Version); err != nil {
		t.Fatal(err)
	}
	beforeReplay := func() {
		state, err := store.State(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.AdvanceRetention(context.Background(), state.Head, "snapshot://retained"); err != nil {
			t.Fatal(err)
		}
	}
	outbox := &ProjectJobsOutbox{store: store, beforeReplay: beforeReplay}
	state, err := outbox.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := outbox.Replay(context.Background(), Cursor{StreamScope: ProjectJob, ScopeID: state.Head.ScopeID, Epoch: state.Head.Epoch, Sequence: state.Head.Sequence - 1}, 1)
	if err != nil || result.Reset == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Reset.Cursor.Sequence != state.Head.Sequence || result.Reset.SnapshotURI != "snapshot://retained" {
		t.Fatalf("reset=%+v", result.Reset)
	}
}

func TestProjectJobsOutboxNilGuardsAllMethods(t *testing.T) {
	var outbox *ProjectJobsOutbox
	if _, err := outbox.State(context.Background()); err != ErrNilOutbox {
		t.Fatalf("state error=%v", err)
	}
	if _, err := outbox.Replay(context.Background(), Cursor{}, 1); err != ErrNilOutbox {
		t.Fatalf("replay error=%v", err)
	}
	if _, err := outbox.PublisherWatermark(context.Background()); err != ErrNilOutbox {
		t.Fatalf("watermark error=%v", err)
	}
	if err := outbox.SetPublisherWatermark(context.Background(), Cursor{}); err != ErrNilOutbox {
		t.Fatalf("set watermark error=%v", err)
	}
}
