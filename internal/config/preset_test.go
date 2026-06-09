package config

import "testing"

func TestPresetByName(t *testing.T) {
	if p := PresetByName("fast"); p.EmbedModel != "nomic-embed-text" {
		t.Fatalf("fast embed = %q", p.EmbedModel)
	}
	if p := PresetByName("max"); p.EmbedModel != "qwen3-embedding:8b" {
		t.Fatalf("max embed = %q", p.EmbedModel)
	}
	// Unknown names fall back to the default tier.
	if p := PresetByName("nope"); p.Name != DefaultTier {
		t.Fatalf("fallback tier = %q, want %q", p.Name, DefaultTier)
	}
	// Default() uses the default tier's models.
	if Default().AI.EmbedModel != PresetByName(DefaultTier).EmbedModel {
		t.Fatal("Default AI embed should match the default tier")
	}
}
