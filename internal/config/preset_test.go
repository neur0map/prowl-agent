package config

import "testing"

func TestPresetByName(t *testing.T) {
	if p := PresetByName("fast"); p.AssistModel != "gemma3:1b" {
		t.Fatalf("fast assist = %q", p.AssistModel)
	}
	if p := PresetByName("max"); p.AssistModel != "gemma4:e4b" {
		t.Fatalf("max assist = %q", p.AssistModel)
	}
	// Unknown names fall back to the default tier.
	if p := PresetByName("nope"); p.Name != DefaultTier {
		t.Fatalf("fallback tier = %q, want %q", p.Name, DefaultTier)
	}
	// Default() uses the default tier's assist model.
	if Default().AI.AssistModel != PresetByName(DefaultTier).AssistModel {
		t.Fatal("Default AI assist should match the default tier")
	}
}

// A tier picks only the optional rewrite/rerank model. Embeddings are bundled
// into the binary, so no tier may name an embedding model as its assist model:
// that would make the AI tier silently unable to generate or rerank.
func TestPresetsNameNoEmbeddingModels(t *testing.T) {
	for _, p := range Presets {
		if p.AssistModel == "" {
			t.Fatalf("tier %q has no assist model", p.Name)
		}
		for _, embedder := range KnownEmbedModels {
			if base, _, _ := splitTag(p.AssistModel); base == embedder {
				t.Fatalf("tier %q assist model %q is an embedding model", p.Name, p.AssistModel)
			}
		}
	}
}

// splitTag separates an Ollama "model:tag" reference.
func splitTag(model string) (base, tag string, tagged bool) {
	for i := range len(model) {
		if model[i] == ':' {
			return model[:i], model[i+1:], true
		}
	}
	return model, "", false
}
