package events

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCursorScopeJSONRoundTripAndValidation(t *testing.T) {
	original := Cursor{
		StreamScope: ProjectJob,
		ScopeID:     "workspace-a",
		Epoch:       7,
		Sequence:    42,
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal cursor: %v", err)
	}
	if got, want := string(encoded), `{"stream_scope":"project-job","scope_id":"workspace-a","epoch":7,"sequence":42}`; got != want {
		t.Fatalf("marshal cursor = %s, want %s", got, want)
	}

	var decoded Cursor
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal cursor: %v", err)
	}
	if decoded != original {
		t.Fatalf("round trip cursor = %#v, want %#v", decoded, original)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("valid cursor: %v", err)
	}

	for name, cursor := range map[string]Cursor{
		"empty stream scope": {ScopeID: "workspace-a", Epoch: 1},
		"unknown stream scope": {StreamScope: StreamScope("other"), ScopeID: "workspace-a", Epoch: 1},
		"empty scope ID": {StreamScope: ProjectJob, Epoch: 1},
		"zero epoch": {StreamScope: ProjectJob, ScopeID: "workspace-a"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := cursor.Validate(); err == nil {
				t.Fatal("Validate() succeeded for an invalid cursor")
			}
		})
	}
}

func TestCursorScopeComparisonRejectsCrossStream(t *testing.T) {
	base := Cursor{StreamScope: ProjectJob, ScopeID: "workspace-a", Epoch: 3, Sequence: 9}
	later := base
	later.Sequence++

	if got, err := base.Compare(later); err != nil || got >= 0 {
		t.Fatalf("Compare(later) = (%d, %v), want (<0, nil)", got, err)
	}
	if got, err := later.Compare(base); err != nil || got <= 0 {
		t.Fatalf("Compare(base) = (%d, %v), want (>0, nil)", got, err)
	}
	if got, err := base.Compare(base); err != nil || got != 0 {
		t.Fatalf("Compare(same) = (%d, %v), want (0, nil)", got, err)
	}

	for name, other := range map[string]Cursor{
		"scope":    {StreamScope: StreamScope("other"), ScopeID: base.ScopeID, Epoch: base.Epoch, Sequence: base.Sequence},
		"scope ID": {StreamScope: ProjectJob, ScopeID: "workspace-b", Epoch: base.Epoch, Sequence: base.Sequence},
		"epoch":    {StreamScope: ProjectJob, ScopeID: base.ScopeID, Epoch: base.Epoch + 1, Sequence: base.Sequence},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := base.Compare(other); !errors.Is(err, ErrCursorStreamMismatch) {
				t.Fatalf("Compare(%#v) error = %v, want ErrCursorStreamMismatch", other, err)
			}
		})
	}
}
