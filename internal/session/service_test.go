package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/agent"
	"github.com/prowl-agent/prowl-agent/internal/operations"
	"github.com/prowl-agent/prowl-agent/internal/profile"
)

func newTestService(t *testing.T) (Service, *operations.Store) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := operations.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewService(store, operations.SurfaceCLI), store
}

// testSnapshot builds a valid immutable B0.2 snapshot with an included secret
// reference (never a secret value) so exposure redaction can be exercised.
func testSnapshot(t *testing.T) *profile.Snapshot {
	t.Helper()
	input := profile.SnapshotInput{
		Provider:             profile.ProviderModel{ProviderID: "fake", ModelID: "model-a", MaxInputTokens: 8192, MaxOutputTokens: 1024},
		CoreVersion:          profile.CoreProwlV1,
		PrincipalID:          "principal-local",
		Profile:              profile.Local(),
		Policy:               profile.PolicyInput{Permission: "read-only", Approval: "explicit", Readiness: "ready"},
		ToolSchemaGeneration: "tools-7",
		Sources: []profile.SourceInput{
			{ID: "task:brief", Kind: profile.TaskInstructionSource, Body: "Implement the assigned slice.", Provenance: profile.TaskProvenance, Scope: profile.TaskScope, Included: true, Reason: "assigned"},
			{ID: "system-security", Kind: profile.SystemPolicySource, Body: "Never reveal secrets.", Provenance: profile.BuiltinProvenance, Scope: profile.GlobalScope, Included: true, Reason: "required"},
			{ID: "secret:provider", Kind: profile.SecretReferenceSource, SecretReference: "OPENAI_API_KEY", Provenance: profile.EnvironmentProvenance, Scope: profile.SessionScope, Included: true, Reason: "provider credential reference"},
			{ID: "user-profile:concise", Kind: profile.UserProfileSource, Body: "Prefer concise answers.", Provenance: profile.UserSelectedProvenance, Scope: profile.UserScope, Included: true, Reason: "selected"},
		},
		Tools: []profile.ToolSchemaInput{
			{ID: "search", Schema: []byte(`{"required":["query"],"properties":{"query":{"type":"string"}},"type":"object","additionalProperties":false}`)},
		},
		Skills: []profile.SkillMetadataInput{
			{ID: "skill:test", Name: "test-driven-development", Description: "Write a failing test first.", ContentHash: testContentHash("RED then GREEN."), Root: "profile/skills", ForcePreload: true},
		},
		PreloadedSkills: []profile.SkillBodyInput{{ID: "skill:test", Body: "RED then GREEN."}},
	}
	snapshot, err := profile.NewSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func testContentHash(body string) string {
	digest := sha256.Sum256([]byte(body))
	return hex.EncodeToString(digest[:])
}

func testPinned(t *testing.T) (snapshotBytes, exposureBytes []byte) {
	t.Helper()
	snapshot := testSnapshot(t)
	manifest, err := agent.NewExposureManifest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.CanonicalBytes(), manifest.CanonicalBytes()
}

func TestCreateTurnResume(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	snapshotBytes, exposureBytes := testPinned(t)

	created, err := svc.CreateSession(ctx, CreateSessionRequest{SnapshotBytes: snapshotBytes, ExposureBytes: exposureBytes})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.Status != SessionActive {
		t.Fatalf("created session=%+v", created)
	}
	if created.SnapshotID == "" || created.ExposureID == "" {
		t.Fatalf("missing pinned ids: %+v", created)
	}
	if !bytes.Equal(created.SnapshotBytes, snapshotBytes) {
		t.Fatal("created session did not pin exact snapshot bytes")
	}

	turnReq := AppendTurnRequest{
		SessionID:       created.ID,
		IdempotencyKey:  "turn-key-1",
		ExpectedVersion: 1,
		RunID:           "run-1",
		Status:          TurnSucceeded,
		Entries: []TurnEntryInput{
			{Kind: EntryMessage, Body: "resume the port", Metadata: EntryMetadata{Role: "user"}},
			{Kind: EntryToolCall, Body: `{"query":"session"}`, Metadata: EntryMetadata{ToolName: "search_context", ToolCallID: "call-1"}},
			{Kind: EntryToolResult, Body: "found 3 sources", Metadata: EntryMetadata{ToolName: "search_context", ToolCallID: "call-1"}},
		},
		Usage: Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}
	turn, err := svc.AppendTurn(ctx, turnReq)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Ordinal != 1 || turn.ExpectedVersion != 1 || turn.ResultingVersion != 2 {
		t.Fatalf("turn versioning=%+v", turn)
	}
	if len(turn.Entries) != 3 || turn.Entries[0].Kind != EntryMessage || turn.Entries[1].Kind != EntryToolCall || turn.Entries[2].Kind != EntryToolResult {
		t.Fatalf("turn entries=%+v", turn.Entries)
	}
	if turn.Entries[0].Ordinal != 1 || turn.Entries[1].Ordinal != 2 || turn.Entries[2].Ordinal != 3 {
		t.Fatalf("entry ordinals out of order: %+v", turn.Entries)
	}
	if turn.CompletedAt == nil {
		t.Fatal("terminal turn missing completion time")
	}
	if turn.Usage.TotalTokens != 15 {
		t.Fatalf("usage=%+v", turn.Usage)
	}

	// Idempotent replay: same key returns the same turn without advancing state.
	replay, err := svc.AppendTurn(ctx, turnReq)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != turn.ID || replay.ResultingVersion != 2 {
		t.Fatalf("idempotent replay produced a new turn: %+v", replay)
	}

	// Optimistic concurrency: a stale expected version with a fresh key conflicts.
	if _, err := svc.AppendTurn(ctx, AppendTurnRequest{SessionID: created.ID, IdempotencyKey: "turn-key-2", ExpectedVersion: 1, RunID: "run-2", Status: TurnSucceeded, Entries: []TurnEntryInput{{Kind: EntryMessage, Body: "stale", Metadata: EntryMetadata{Role: "user"}}}}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale expected version error=%v", err)
	}

	// Resume returns the pinned snapshot bytes, never re-resolving state.
	resumed, err := svc.GetSession(ctx, GetSessionRequest{SessionID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Version != 2 || resumed.Status != SessionActive {
		t.Fatalf("resumed=%+v", resumed)
	}
	if !bytes.Equal(resumed.SnapshotBytes, snapshotBytes) {
		t.Fatal("resume did not return the pinned snapshot")
	}
	reopened, err := profile.OpenSnapshot(resumed.SnapshotBytes)
	if err != nil || reopened.ID() != created.SnapshotID {
		t.Fatalf("pinned snapshot did not reopen: id=%q err=%v", created.SnapshotID, err)
	}
	if len(resumed.Turns) != 1 || len(resumed.Turns[0].Entries) != 3 {
		t.Fatalf("resumed ledger=%+v", resumed.Turns)
	}

	// A missing session is a typed not-found.
	if _, err := svc.GetSession(ctx, GetSessionRequest{SessionID: "does-not-exist"}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session error=%v", err)
	}
}

func TestRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	ctx := context.Background()
	snapshotBytes, exposureBytes := testPinned(t)

	store1, err := operations.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	svc1 := NewService(store1, operations.SurfaceCLI)
	created, err := svc1.CreateSession(ctx, CreateSessionRequest{SnapshotBytes: snapshotBytes, ExposureBytes: exposureBytes})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc1.AppendTurn(ctx, AppendTurnRequest{SessionID: created.ID, IdempotencyKey: "k1", ExpectedVersion: 1, RunID: "r1", Status: TurnSucceeded, Entries: []TurnEntryInput{{Kind: EntryMessage, Body: "first", Metadata: EntryMetadata{Role: "user"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc1.AppendTurn(ctx, AppendTurnRequest{SessionID: created.ID, IdempotencyKey: "k2", ExpectedVersion: 2, RunID: "r2", Status: TurnRunning, Entries: []TurnEntryInput{{Kind: EntryMessage, Body: "second", Metadata: EntryMetadata{Role: "assistant"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := store1.Close(); err != nil {
		t.Fatal(err)
	}

	store2, err := operations.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	svc2 := NewService(store2, operations.SurfaceCLI)

	got, err := svc2.GetSession(ctx, GetSessionRequest{SessionID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 {
		t.Fatalf("restart version=%d", got.Version)
	}
	if len(got.Turns) != 2 || got.Turns[0].Ordinal != 1 || got.Turns[1].Ordinal != 2 {
		t.Fatalf("restart turn order=%+v", got.Turns)
	}
	if got.Turns[0].RunID != "r1" || got.Turns[1].RunID != "r2" {
		t.Fatalf("restart run identity=%+v", got.Turns)
	}
	if got.Turns[1].Status != TurnRunning || got.Turns[1].CompletedAt != nil {
		t.Fatalf("non-terminal turn state not preserved: %+v", got.Turns[1])
	}
	if got.PrincipalID != created.PrincipalID || got.OwnerPrincipalID != created.OwnerPrincipalID || got.SurfaceID != string(operations.SurfaceCLI) {
		t.Fatalf("restart attribution=%+v", got)
	}
	if !bytes.Equal(got.SnapshotBytes, snapshotBytes) {
		t.Fatal("restart did not preserve frozen snapshot bytes")
	}
	exposure, err := svc2.GetExposure(ctx, GetSessionRequest{SessionID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(exposure.ExposureBytes, exposureBytes) {
		t.Fatal("restart did not preserve frozen exposure bytes")
	}
}

func TestExposure(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	snapshotBytes, exposureBytes := testPinned(t)

	created, err := svc.CreateSession(ctx, CreateSessionRequest{SnapshotBytes: snapshotBytes, ExposureBytes: exposureBytes})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetExposure(ctx, GetSessionRequest{SessionID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.ExposureBytes, exposureBytes) {
		t.Fatal("exposure did not return the pinned bytes")
	}
	if got.ExposureID != created.ExposureID || got.SnapshotID != created.SnapshotID {
		t.Fatalf("exposure ids=%+v vs %+v", got, created)
	}

	manifest, err := agent.OpenExposureManifest(got.ExposureBytes)
	if err != nil {
		t.Fatal(err)
	}
	foundRef := false
	for _, reference := range manifest.SecretReferences() {
		if reference == "OPENAI_API_KEY" {
			foundRef = true
		}
	}
	if !foundRef {
		t.Fatalf("secret reference missing from exposure: %v", manifest.SecretReferences())
	}
	// Exposure carries authority hashes only; source bodies never leak.
	for _, leaked := range []string{"Prefer concise answers.", "Never reveal secrets.", "Implement the assigned slice."} {
		if bytes.Contains(got.ExposureBytes, []byte(leaked)) {
			t.Fatalf("exposure leaked source body %q", leaked)
		}
	}

	// Repeated reads are byte-identical: exposure never re-resolves mutable state.
	again, err := svc.GetExposure(ctx, GetSessionRequest{SessionID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again.ExposureBytes, exposureBytes) {
		t.Fatal("exposure re-resolved instead of returning pinned bytes")
	}
	if _, err := svc.GetExposure(ctx, GetSessionRequest{SessionID: "missing"}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing exposure error=%v", err)
	}
}

func TestActor(t *testing.T) {
	// Strict request decoding rejects smuggled authoritative actor fields.
	if _, err := DecodeAppendTurnRequest([]byte(`{"session_id":"s","owner_principal_id":"evil"}`)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("turn owner smuggle accepted: %v", err)
	}
	if _, err := DecodeAppendTurnRequest([]byte(`{"session_id":"s","principal_id":"evil"}`)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("turn principal smuggle accepted: %v", err)
	}
	if _, err := DecodeCreateSessionRequest([]byte(`{"owner_principal_id":"evil"}`)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("create owner smuggle accepted: %v", err)
	}
	if _, err := DecodeCreateSessionRequest([]byte(`{"surface_id":"workbench"}`)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("create surface smuggle accepted: %v", err)
	}
	// A legitimate turn body still decodes cleanly.
	if _, err := DecodeAppendTurnRequest([]byte(`{"session_id":"s","idempotency_key":"k","expected_version":1,"run_id":"r","status":"succeeded","entries":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`)); err != nil {
		t.Fatalf("legitimate turn rejected: %v", err)
	}

	// Persisted attribution is derived server-side, never from the request.
	svc, store := newTestService(t)
	ctx := context.Background()
	principal, err := store.ResolveLocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshotBytes, exposureBytes := testPinned(t)
	created, err := svc.CreateSession(ctx, CreateSessionRequest{SnapshotBytes: snapshotBytes, ExposureBytes: exposureBytes})
	if err != nil {
		t.Fatal(err)
	}
	if created.PrincipalID != principal.ID || created.OwnerPrincipalID != principal.ID {
		t.Fatalf("session principal not server-derived: %+v", created)
	}
	if created.SurfaceID != string(operations.SurfaceCLI) || created.AuthorizationScope != "local" || created.RequestedProfileID != "local" || created.ResolvedProfileID != "local" {
		t.Fatalf("session attribution not server-derived: %+v", created)
	}
	turn, err := svc.AppendTurn(ctx, AppendTurnRequest{SessionID: created.ID, IdempotencyKey: "k", ExpectedVersion: 1, RunID: "r", Status: TurnSucceeded, Entries: []TurnEntryInput{{Kind: EntryMessage, Body: "x", Metadata: EntryMetadata{Role: "user"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if turn.PrincipalID != principal.ID || turn.OwnerPrincipalID != principal.ID || turn.SurfaceID != string(operations.SurfaceCLI) {
		t.Fatalf("turn attribution not server-derived: %+v", turn)
	}
}

func TestSessionDocumentFixtureRoundTrip(t *testing.T) {
	data, err := os.ReadFile("testdata/session_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var view SessionView
	if err := decoder.Decode(&view); err != nil {
		t.Fatalf("fixture decode: %v", err)
	}

	// Hand-authored expectations pin the wire compatibility contract.
	if view.Version != 2 || view.Status != SessionActive {
		t.Fatalf("fixture session=%+v", view)
	}
	if view.SurfaceID != "cli" || view.AuthorizationScope != "local" || view.RequestedProfileID != "local" {
		t.Fatalf("fixture attribution=%+v", view)
	}
	if len(view.Turns) != 1 {
		t.Fatalf("fixture turns=%d", len(view.Turns))
	}
	turn := view.Turns[0]
	if turn.Ordinal != 1 || turn.ExpectedVersion != 1 || turn.ResultingVersion != 2 || turn.Status != TurnSucceeded {
		t.Fatalf("fixture turn=%+v", turn)
	}
	if len(turn.Entries) != 3 || turn.Entries[0].Kind != EntryMessage || turn.Entries[1].Kind != EntryToolCall || turn.Entries[2].Kind != EntryToolResult {
		t.Fatalf("fixture entries=%+v", turn.Entries)
	}
	if turn.Entries[1].Metadata.ToolName != "search_context" {
		t.Fatalf("fixture tool metadata=%+v", turn.Entries[1].Metadata)
	}

	// Stable round-trip through the wire types without production computation.
	encoded, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	roundDecoder := json.NewDecoder(bytes.NewReader(encoded))
	roundDecoder.DisallowUnknownFields()
	var again SessionView
	if err := roundDecoder.Decode(&again); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	if !reflect.DeepEqual(view, again) {
		t.Fatalf("round trip mismatch:\n first=%+v\nsecond=%+v", view, again)
	}
}
