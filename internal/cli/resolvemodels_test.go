package cli

import (
	"context"
	"fmt"
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

// fakeEmbedder is a stub Embedder for wiring tests (no model files or network).
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(context.Context, []string) ([][]float32, error) { return nil, nil }
func (fakeEmbedder) EmbedModelID() string                                 { return "static:test" }

// stubEmbedder swaps the package embedder loader for the duration of a test.
func stubEmbedder(t *testing.T, emb assist.Embedder, err error) {
	t.Helper()
	prev := loadEmbedder
	loadEmbedder = func(context.Context) (assist.Embedder, error) { return emb, err }
	t.Cleanup(func() { loadEmbedder = prev })
}

// With no local Ollama embed model, embeddings still come from the in-process
// static model, so search stays semantic even with no agent -- the backend is a
// Composite embedder, never nil.
func TestMaybeInferencerStaticWhenNoOllamaModel(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // hide any claude/codex/omp
	stubEmbedder(t, fakeEmbedder{}, nil)
	srv := tagsServer(t, `{"models":[{"name":"some-other-model:latest"}]}`)
	cfg := config.Config{AI: config.AI{Enabled: true, EmbedModel: "nomic-embed-text", OllamaURL: srv.URL}}
	c, ok := maybeInferencer(context.Background(), cfg).(assist.Composite)
	if !ok {
		t.Fatal("want assist.Composite (static embeddings) when no Ollama model")
	}
	if c.Assist != nil {
		t.Fatalf("Assist = %T, want nil (no agent available)", c.Assist)
	}
}

// With no Ollama model but a coding-agent command, embeddings come from the
// static model and rewrite/rerank from the agent: a Composite carrying both.
func TestMaybeInferencerStaticPlusAgent(t *testing.T) {
	stubEmbedder(t, fakeEmbedder{}, nil)
	srv := tagsServer(t, `{"models":[{"name":"some-other-model:latest"}]}`)
	cfg := config.Config{AI: config.AI{Enabled: true, EmbedModel: "nomic-embed-text", OllamaURL: srv.URL, AgentCommand: "go"}}
	c, ok := maybeInferencer(context.Background(), cfg).(assist.Composite)
	if !ok {
		t.Fatal("want assist.Composite")
	}
	if _, ok := c.Assist.(*assist.AgentCLI); !ok {
		t.Fatalf("Assist = %T, want *assist.AgentCLI", c.Assist)
	}
}

// Only when the embedder itself is unavailable (e.g. offline first run) does the
// backend fall back to agent-only rewrite/rerank.
func TestMaybeInferencerAgentOnlyWhenEmbedderUnavailable(t *testing.T) {
	stubEmbedder(t, nil, fmt.Errorf("offline"))
	srv := tagsServer(t, `{"models":[{"name":"some-other-model:latest"}]}`)
	cfg := config.Config{AI: config.AI{Enabled: true, EmbedModel: "nomic-embed-text", OllamaURL: srv.URL, AgentCommand: "go"}}
	if _, ok := maybeInferencer(context.Background(), cfg).(*assist.AgentCLI); !ok {
		t.Fatal("want *assist.AgentCLI when embedder unavailable but agent present")
	}
}

// With no embedder and no agent, search is structural (nil).
func TestMaybeInferencerStructuralWhenNothing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	stubEmbedder(t, nil, fmt.Errorf("offline"))
	srv := tagsServer(t, `{"models":[{"name":"some-other-model:latest"}]}`)
	cfg := config.Config{AI: config.AI{Enabled: true, EmbedModel: "nomic-embed-text", OllamaURL: srv.URL}}
	if inf := maybeInferencer(context.Background(), cfg); inf != nil {
		t.Fatalf("want nil (structural), got %T", inf)
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
