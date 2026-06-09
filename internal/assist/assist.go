// Package assist is the semantic-assist layer. It provides a local Inferencer
// (Ollama) used internally by retrieval tools to embed and generate.
//
// In M1 the interface, client, and availability detection exist and are tested,
// and the init wizard uses Available() to guide setup. Wiring embeddings into
// the query path (vector search + reranking) lands in M2.
package assist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Inferencer is the provider-agnostic local model interface. Its outputs are
// deliberately narrow: embeddings and constrained text generation. It never
// makes decisions; callers (retrieval tools) use it only to rank/compact.
type Inferencer interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Generate(ctx context.Context, prompt string) (string, error)
}

// Ollama talks to a local Ollama daemon over HTTP.
type Ollama struct {
	BaseURL    string
	EmbedModel string
	GenModel   string
	HTTP       *http.Client
}

var _ Inferencer = (*Ollama)(nil)

// NewOllama builds a client; baseURL defaults to http://localhost:11434.
func NewOllama(baseURL, embedModel, genModel string) *Ollama {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Ollama{
		BaseURL:    baseURL,
		EmbedModel: embedModel,
		GenModel:   genModel,
		HTTP:       &http.Client{Timeout: 60 * time.Second},
	}
}

// Available reports whether the Ollama daemon is reachable.
func (o *Ollama) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.BaseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Embed returns one vector per input text via /api/embed.
func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	body := map[string]any{"model": o.EmbedModel, "input": texts}
	if err := o.post(ctx, "/api/embed", body, &out); err != nil {
		return nil, err
	}
	return out.Embeddings, nil
}

// Generate runs a deterministic (temperature 0) completion via /api/generate.
func (o *Ollama) Generate(ctx context.Context, prompt string) (string, error) {
	var out struct {
		Response string `json:"response"`
	}
	body := map[string]any{
		"model": o.GenModel, "prompt": prompt, "stream": false,
		"options": map[string]any{"temperature": 0},
	}
	if err := o.post(ctx, "/api/generate", body, &out); err != nil {
		return "", err
	}
	return out.Response, nil
}

func (o *Ollama) post(ctx context.Context, path string, body, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
