package agent

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

const expectedExposureHash = "770473f3304232712336ba6f6be89f67a7928e18532c4b743a2430e15546d8ef"

func TestExposureManifestMatchesHandAuthoredFixtureAndRedactsSecrets(t *testing.T) {
	snapshot := testSnapshot(t)
	manifest, err := NewExposureManifest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/exposure.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.CanonicalBytes(); !bytes.Equal(got, want) {
		t.Fatalf("exposure mismatch\n got: %s\nwant: %s", got, want)
	}
	if manifest.ID() != expectedExposureHash {
		t.Fatalf("manifest hash=%q want %q", manifest.ID(), expectedExposureHash)
	}
	if manifest.SnapshotID() != snapshot.ID() {
		t.Fatalf("snapshot id=%q want %q", manifest.SnapshotID(), snapshot.ID())
	}
	// Secret values must not appear anywhere in the exposure bytes.
	if strings.Contains(string(manifest.CanonicalBytes()), "secret-value") {
		t.Fatal("exposure included a secret value")
	}
	secretRefs := manifest.SecretReferences()
	if len(secretRefs) != 1 || secretRefs[0] != "OPENAI_API_KEY" {
		t.Fatalf("secret references=%v", secretRefs)
	}
	// Tool schemas must be listed with IDs and hashes.
	toolSchemas := manifest.ToolSchemas()
	if len(toolSchemas) != 2 {
		t.Fatalf("expected 2 tool schemas, got %d: %v", len(toolSchemas), toolSchemas)
	}
	if toolSchemas[0].ID != "read" || toolSchemas[1].ID != "search" {
		t.Fatalf("tool schemas misordered or misnamed: %v", toolSchemas)
	}
	for _, ts := range toolSchemas {
		if !validHash(ts.Hash) {
			t.Fatalf("tool schema %q has invalid hash %q", ts.ID, ts.Hash)
		}
	}
	// Tool set hash must be a valid SHA-256.
	if !validHash(manifest.ToolSetHash()) {
		t.Fatalf("tool set hash invalid: %q", manifest.ToolSetHash())
	}
	// Skills must be listed.
	skills := manifest.Skills()
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d: %v", len(skills), skills)
	}
	if skills[0].ID != "skill:discover" || skills[1].ID != "skill:test" {
		t.Fatalf("skills misordered or misnamed: %v", skills)
	}
	// Preloaded skill bodies.
	bodies := manifest.PreloadedSkillBodies()
	if len(bodies) != 1 || bodies[0].ID != "skill:test" {
		t.Fatalf("preloaded skill bodies=%v", bodies)
	}
	if !validHash(bodies[0].Hash) {
		t.Fatalf("preloaded body hash invalid")
	}
	// Prompt hash must be a valid SHA-256.
	if !validHash(manifest.PromptHash()) {
		t.Fatalf("prompt hash invalid: %q", manifest.PromptHash())
	}
	// Included sources: policy:active, system-security, profile:local, secret:provider,
	// user-profile:concise, memory:project-tone, project:AGENTS.md, task:brief
	included := manifest.Included()
	omitted := manifest.Omitted()
	if len(omitted) != 1 || omitted[0].ID != "web:result-1" || omitted[0].Reason != "untrusted content not preloaded" {
		t.Fatalf("omitted=%+v", omitted)
	}
	if included[0].ID != "policy:active" || included[0].Precedence != "executable_system" {
		t.Fatalf("policy authority missing or misordered: %+v", included)
	}
	if included[2].ID != "profile:local" || included[2].Trust != "profile" {
		t.Fatalf("profile authority missing or misordered: %+v", included)
	}
	// Mutating accessors must not change the frozen manifest.
	included[0].ID = "mutated"
	omitted[0].ID = "mutated"
	toolSchemas[0].ID = "mutated"
	skills[0].ID = "mutated"
	bodies[0].ID = "mutated"
	secretRefs[0] = "mutated"
	returnedBytes := manifest.CanonicalBytes()
	returnedBytes[0] = 'x'
	if !bytes.Equal(manifest.CanonicalBytes(), want) ||
		manifest.Included()[0].ID == "mutated" ||
		manifest.Omitted()[0].ID == "mutated" ||
		manifest.ToolSchemas()[0].ID == "mutated" ||
		manifest.SecretReferences()[0] == "mutated" {
		t.Fatal("exposure accessors mutated the frozen manifest")
	}
}

func TestExposureManifestCanonicalRoundTripRejectsTampering(t *testing.T) {
	snapshot := testSnapshot(t)
	manifest, err := NewExposureManifest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	canonical := manifest.CanonicalBytes()
	reopened, err := OpenExposureManifest(canonical)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if !bytes.Equal(reopened.CanonicalBytes(), canonical) {
		t.Fatal("reopened exposure bytes differ")
	}
	// Tampered bytes must be rejected.
	tampered := append([]byte(nil), canonical...)
	tampered = bytes.Replace(tampered, []byte(`"snapshot_id"`), []byte(`"snapshott_id"`), 1)
	if _, err := OpenExposureManifest(tampered); err == nil {
		t.Fatal("tampered exposure was accepted")
	}
}
