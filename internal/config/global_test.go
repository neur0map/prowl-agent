package config

import "testing"

func TestGlobalConfigRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Absent file yields zero-value defaults, not an error.
	g, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal (absent): %v", err)
	}
	if g.AIEnabled {
		t.Fatalf("absent global config should default AIEnabled=false, got %+v", g)
	}

	want := GlobalConfig{AIEnabled: true, Tier: "smart", EmbedModel: "e-model", AssistModel: "a-model"}
	if err := SaveGlobal(want); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	got, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}
}
