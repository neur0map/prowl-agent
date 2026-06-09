package config

import "testing"

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := Default()
	c.AI.Enabled = true
	if err := Save(dir, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AI.Enabled || got.AI.AssistModel != "gemma3:4b" || got.AI.EmbedModel != "qwen3-embedding:0.6b" {
		t.Fatalf("roundtrip = %+v", got.AI)
	}
	if len(got.Languages) == 0 {
		t.Fatal("languages not persisted")
	}

	// Missing config returns defaults (AI disabled).
	if d, _ := Load(t.TempDir()); d.AI.Enabled {
		t.Fatal("default config should have AI disabled")
	}

	if err := SaveRules(dir, DefaultRules()); err != nil {
		t.Fatal(err)
	}
	r, err := LoadRules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rule) != 3 {
		t.Fatalf("rules = %d, want 3", len(r.Rule))
	}
}
