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
