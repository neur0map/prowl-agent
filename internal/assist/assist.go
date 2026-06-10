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
	"strconv"
	"strings"
	"time"
)

// Inferencer is the provider-agnostic local model interface. Its outputs are
// deliberately narrow: embeddings, constrained generation, and rank orderings.
// It never makes decisions; callers (retrieval tools) use it only to rank or
// compact already-retrieved candidates.
type Inferencer interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Generate(ctx context.Context, prompt string) (string, error)
	Rerank(ctx context.Context, query string, docs []string) ([]int, error)
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

// Warm loads model into memory and pins it for keepAlive (e.g. "30m") via a tiny
// embed, so the first real query after init is hot instead of paying a cold
// start. Best-effort: callers ignore the error.
func (o *Ollama) Warm(ctx context.Context, model, keepAlive string) error {
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	body := map[string]any{"model": model, "input": " ", "keep_alive": keepAlive}
	return o.post(ctx, "/api/embed", body, &out)
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

// Rerank asks the assist model to order docs by relevance to query and returns
// the resulting index permutation. Its output is constrained to an ordering: the
// model never adds, removes, or rewrites results. Any indices the model omits are
// appended in their original order, so the result is always a full permutation.
func (o *Ollama) Rerank(ctx context.Context, query string, docs []string) ([]int, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	var b strings.Builder
	b.WriteString("Order the snippets by relevance to the query. ")
	b.WriteString("Reply with only the snippet numbers, most relevant first, comma-separated.\n")
	b.WriteString("Query: ")
	b.WriteString(query)
	b.WriteString("\n")
	for i, d := range docs {
		if len(d) > 240 {
			d = d[:240]
		}
		fmt.Fprintf(&b, "[%d] %s\n", i, strings.ReplaceAll(d, "\n", " "))
	}
	resp, err := o.Generate(ctx, b.String())
	if err != nil {
		return nil, err
	}
	return parseOrder(resp, len(docs)), nil
}

// parseOrder extracts a permutation of [0,n) from free-form model text, keeping
// the first occurrence of each valid index and appending any missing indices.
func parseOrder(resp string, n int) []int {
	seen := make([]bool, n)
	var order []int
	for _, tok := range strings.FieldsFunc(resp, func(r rune) bool { return r < '0' || r > '9' }) {
		v, err := strconv.Atoi(tok)
		if err != nil || v < 0 || v >= n || seen[v] {
			continue
		}
		seen[v] = true
		order = append(order, v)
	}
	for i := 0; i < n; i++ {
		if !seen[i] {
			order = append(order, i)
		}
	}
	return order
}

// Models lists the model names available to the daemon (via /api/tags).
func (o *Ollama) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.BaseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama /api/tags: status %d", resp.StatusCode)
	}
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	names := make([]string, len(out.Models))
	for i, m := range out.Models {
		names[i] = m.Name
	}
	return names, nil
}

// HasModel reports whether a model (with or without an explicit tag) is present.
func (o *Ollama) HasModel(ctx context.Context, name string) bool {
	have, err := o.Models(ctx)
	if err != nil {
		return false
	}
	return matchModel(have, name)
}

// matchModel tolerates Ollama's implicit :latest tag and bare-name requests.
func matchModel(have []string, want string) bool {
	for _, h := range have {
		if h == want || h == want+":latest" {
			return true
		}
		if !strings.Contains(want, ":") {
			if i := strings.IndexByte(h, ':'); i >= 0 && h[:i] == want {
				return true
			}
		}
	}
	return false
}
