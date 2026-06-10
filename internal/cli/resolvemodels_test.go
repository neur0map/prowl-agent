package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/assist"
	"github.com/prowl-agent/prowl-agent/internal/config"
)

// tagsServer serves a fixed /api/tags body so model detection can be tested
// without a live Ollama.
func tagsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// When the tier preset models are not installed but a usable embedding model is,
// resolveModels substitutes the installed models instead of the absent preset.
func TestResolveModelsPrefersInstalled(t *testing.T) {
	srv := tagsServer(t, `{"models":[{"name":"nomic-embed-text:latest"},{"name":"qwen3:0.6b"}]}`)
	oll := assist.NewOllama(srv.URL, "embeddinggemma", "gemma3:1b")
	embed, gen := resolveModels(context.Background(), oll, config.PresetByName("fast"))
	if embed != "nomic-embed-text:latest" {
		t.Errorf("embed = %q, want nomic-embed-text:latest", embed)
	}
	if gen != "qwen3:0.6b" {
		t.Errorf("gen = %q, want qwen3:0.6b", gen)
	}
}

// When the preset models are present, resolveModels keeps them.
func TestResolveModelsKeepsPresetWhenInstalled(t *testing.T) {
	srv := tagsServer(t, `{"models":[{"name":"embeddinggemma:latest"},{"name":"gemma3:1b"}]}`)
	oll := assist.NewOllama(srv.URL, "embeddinggemma", "gemma3:1b")
	embed, gen := resolveModels(context.Background(), oll, config.PresetByName("fast"))
	if embed != "embeddinggemma" || gen != "gemma3:1b" {
		t.Errorf("embed=%q gen=%q, want preset embeddinggemma/gemma3:1b", embed, gen)
	}
}

// When Ollama is unreachable, resolveModels falls back to the preset names so
// init can still print the pull instructions.
func TestResolveModelsFallsBackWhenUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NewServeMux())
	url := srv.URL
	srv.Close() // dead endpoint: connections are refused
	oll := assist.NewOllama(url, "embeddinggemma", "gemma3:1b")
	embed, gen := resolveModels(context.Background(), oll, config.PresetByName("fast"))
	if embed != "embeddinggemma" || gen != "gemma3:1b" {
		t.Errorf("embed=%q gen=%q, want preset fallback", embed, gen)
	}
}

// A configured embed model that is not installed must degrade to structural-only
// (nil inferencer), not error, so the server keeps running.
func TestMaybeInferencerDegradesWhenModelMissing(t *testing.T) {
	srv := tagsServer(t, `{"models":[{"name":"some-other-model:latest"}]}`)
	cfg := config.Config{AI: config.AI{Enabled: true, EmbedModel: "nomic-embed-text", OllamaURL: srv.URL}}
	if inf := maybeInferencer(context.Background(), cfg); inf != nil {
		t.Fatal("inferencer should be nil when the embed model is not installed")
	}
}

// When the configured embed model is installed, maybeInferencer returns a client.
func TestMaybeInferencerWhenModelPresent(t *testing.T) {
	srv := tagsServer(t, `{"models":[{"name":"nomic-embed-text:latest"}]}`)
	cfg := config.Config{AI: config.AI{Enabled: true, EmbedModel: "nomic-embed-text", OllamaURL: srv.URL}}
	if inf := maybeInferencer(context.Background(), cfg); inf == nil {
		t.Fatal("inferencer should be set when the embed model is installed")
	}
}
