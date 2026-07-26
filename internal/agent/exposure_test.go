package agent

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

const expectedExposureHash = "face06b0ca9fb57a07358280b8779d3f4450d1a00895b297de9deda43ad4cc56"

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
	if strings.Contains(string(manifest.CanonicalBytes()), "secret-value") || strings.Contains(string(manifest.CanonicalBytes()), "Never reveal secrets.OPENAI") {
		t.Fatal("exposure included a secret value")
	}
	if got := manifest.SecretReferences(); len(got) != 1 || got[0] != "OPENAI_API_KEY" {
		t.Fatalf("secret references=%v", got)
	}
	if got := manifest.ToolSchemaIDs(); !slicesEqual(got, []string{"read", "search"}) {
		t.Fatalf("tool schema ids=%v", got)
	}
	if got := manifest.SkillMetadataIDs(); !slicesEqual(got, []string{"skill:discover", "skill:test"}) {
		t.Fatalf("skill metadata ids=%v", got)
	}
	if got := manifest.PreloadedSkillBodyIDs(); !slicesEqual(got, []string{"skill:test"}) {
		t.Fatalf("preloaded skill ids=%v", got)
	}
	included := manifest.Included()
	omitted := manifest.Omitted()
	if len(included) != 8 || len(omitted) != 1 || omitted[0].ID != "web:result-1" || omitted[0].Reason != "untrusted content not preloaded" {
		t.Fatalf("included=%+v omitted=%+v", included, omitted)
	}
	if included[0].ID != "policy:active" || included[0].Precedence != "executable_system" || included[1].ID != "system-security" || included[2].ID != "profile:local" || included[2].Trust != "profile" {
		t.Fatalf("policy/profile authority missing or misordered: %+v", included)
	}
	included[0].ID = "mutated"
	omitted[0].ID = "mutated"
	toolIDs := manifest.ToolSchemaIDs()
	toolIDs[0] = "mutated"
	metadataIDs := manifest.SkillMetadataIDs()
	metadataIDs[0] = "mutated"
	bodyIDs := manifest.PreloadedSkillBodyIDs()
	bodyIDs[0] = "mutated"
	secretReferences := manifest.SecretReferences()
	secretReferences[0] = "mutated"
	returnedBytes := manifest.CanonicalBytes()
	returnedBytes[0] = 'x'
	if !bytes.Equal(manifest.CanonicalBytes(), want) || manifest.Included()[0].ID == "mutated" || manifest.Omitted()[0].ID == "mutated" || manifest.ToolSchemaIDs()[0] == "mutated" || manifest.SecretReferences()[0] == "mutated" {
		t.Fatal("exposure accessors mutated the frozen manifest")
	}
}

func TestExposureManifestCanonicalRoundTripRejectsTampering(t *testing.T) {
	manifest, err := NewExposureManifest(testSnapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenExposureManifest(manifest.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ID() != manifest.ID() || !bytes.Equal(reopened.CanonicalBytes(), manifest.CanonicalBytes()) {
		t.Fatal("reopened exposure changed canonical identity")
	}
	tampered := append([]byte(nil), manifest.CanonicalBytes()...)
	tampered = bytes.Replace(tampered, []byte(`"snapshot_id"`), []byte(` "snapshot_id"`), 1)
	if _, err := OpenExposureManifest(tampered); err == nil {
		t.Fatal("non-canonical persisted exposure was accepted")
	}
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
