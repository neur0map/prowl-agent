package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/agent"
	"github.com/prowl-agent/prowl-agent/internal/operations"
	"github.com/prowl-agent/prowl-agent/internal/profile"
	"github.com/prowl-agent/prowl-agent/internal/session"
)

func cliTestSnapshot(t *testing.T) *profile.Snapshot {
	t.Helper()
	hash := func(body string) string {
		digest := sha256.Sum256([]byte(body))
		return hex.EncodeToString(digest[:])
	}
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
		},
		Tools: []profile.ToolSchemaInput{
			{ID: "search", Schema: []byte(`{"required":["query"],"properties":{"query":{"type":"string"}},"type":"object","additionalProperties":false}`)},
		},
		Skills: []profile.SkillMetadataInput{
			{ID: "skill:test", Name: "test-driven-development", Description: "Write a failing test first.", ContentHash: hash("RED then GREEN."), Root: "profile/skills", ForcePreload: true},
		},
		PreloadedSkills: []profile.SkillBodyInput{{ID: "skill:test", Body: "RED then GREEN."}},
	}
	snapshot, err := profile.NewSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func writeSnapshotFixtureFiles(t *testing.T) (snapshotPath, exposurePath string) {
	t.Helper()
	snapshot := cliTestSnapshot(t)
	manifest, err := agent.NewExposureManifest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	snapshotPath = filepath.Join(dir, "snapshot.json")
	exposurePath = filepath.Join(dir, "exposure.json")
	if err := os.WriteFile(snapshotPath, snapshot.CanonicalBytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exposurePath, manifest.CanonicalBytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return snapshotPath, exposurePath
}

func TestSessionCLIStartTurnShowResume(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := context.Background()
	store, err := operations.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := session.NewService(store, operations.SurfaceCLI)

	snapshotPath, exposurePath := writeSnapshotFixtureFiles(t)

	var out bytes.Buffer

	start := newSessionStartCmd(svc)
	start.SetOut(&out)
	start.SetErr(&out)
	start.SetArgs([]string{"--snapshot", snapshotPath, "--exposure", exposurePath, "--json"})
	if err := start.Execute(); err != nil {
		t.Fatalf("start: %v out=%s", err, out.String())
	}
	var created session.SessionView
	if err := json.Unmarshal(out.Bytes(), &created); err != nil {
		t.Fatalf("start json %q: %v", out.String(), err)
	}
	if created.ID == "" || created.Version != 1 || created.Status != session.SessionActive {
		t.Fatalf("created=%+v", created)
	}
	if created.SnapshotID == "" || created.SurfaceID != string(operations.SurfaceCLI) {
		t.Fatalf("created attribution=%+v", created)
	}

	turn := newSessionTurnCmd(svc)
	out.Reset()
	turn.SetOut(&out)
	turn.SetErr(&out)
	turn.SetArgs([]string{"--session", created.ID, "--idempotency-key", "k1", "--expected-version", "1", "--run", "r1", "--status", "succeeded", "--message", "hello", "--json"})
	if err := turn.Execute(); err != nil {
		t.Fatalf("turn: %v out=%s", err, out.String())
	}
	var turnView session.TurnView
	if err := json.Unmarshal(out.Bytes(), &turnView); err != nil {
		t.Fatalf("turn json %q: %v", out.String(), err)
	}
	if turnView.Ordinal != 1 || turnView.ResultingVersion != 2 || len(turnView.Entries) != 1 || turnView.Entries[0].Kind != session.EntryMessage {
		t.Fatalf("turnView=%+v", turnView)
	}

	show := newSessionShowCmd(svc)
	out.Reset()
	show.SetOut(&out)
	show.SetErr(&out)
	show.SetArgs([]string{"--session", created.ID, "--json"})
	if err := show.Execute(); err != nil {
		t.Fatalf("show: %v out=%s", err, out.String())
	}
	var shown session.SessionView
	if err := json.Unmarshal(out.Bytes(), &shown); err != nil {
		t.Fatalf("show json %q: %v", out.String(), err)
	}
	if shown.Version != 2 || len(shown.Turns) != 1 || len(shown.Turns[0].Entries) != 1 {
		t.Fatalf("shown=%+v", shown)
	}

	resume := newSessionResumeCmd(svc)
	out.Reset()
	resume.SetOut(&out)
	resume.SetErr(&out)
	resume.SetArgs([]string{"--session", created.ID})
	if err := resume.Execute(); err != nil {
		t.Fatalf("resume: %v out=%s", err, out.String())
	}
	// Resume emits the exact pinned snapshot canonical bytes; they reopen and
	// match the id pinned at creation without re-resolving state.
	snapshot, err := profile.OpenSnapshot(out.Bytes())
	if err != nil {
		t.Fatalf("resume snapshot reopen: %v out=%q", err, out.String())
	}
	if snapshot.ID() != created.SnapshotID {
		t.Fatalf("resume snapshot id=%s want=%s", snapshot.ID(), created.SnapshotID)
	}
}

func TestSessionCLIRequiresSessionID(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := context.Background()
	store, err := operations.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := session.NewService(store, operations.SurfaceCLI)

	show := newSessionShowCmd(svc)
	var out bytes.Buffer
	show.SetOut(&out)
	show.SetErr(&out)
	show.SetArgs(nil)
	if err := show.Execute(); err == nil {
		t.Fatal("show without --session was accepted")
	}
}
