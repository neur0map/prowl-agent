// Package embed is prowl's in-process semantic embedder. It runs a static
// (Model2Vec-style) sentence embedding model in pure Go -- tokenize, look up a
// vector per token, mean-pool, L2-normalize -- with no daemon, no GPU, and no
// API key. This is what makes semantic vector search always-on in every repo an
// agent touches: unlike an Ollama model or a cloud API, the embedder is part of
// the binary's behavior and needs nothing installed.
//
// The model (minishlab/potion-code-16M, a code-tuned static model distilled from
// bge-base-en-v1.5) is fetched once and cached locally; see fetch.go. Embedding
// quality is far below a large neural model but far above lexical FTS: it
// captures meaning, so "how do I refresh the widget" finds RefreshWidget even
// with no shared tokens.
package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
)

// ModelName identifies the embedder in the store's vector metadata, so switching
// embedding backends (e.g. to a local Ollama model) triggers a clean re-embed.
const ModelName = "static:potion-code-16M"

// Model is a loaded static embedder: a WordPiece vocabulary, a token->vector
// lookup matrix, optional per-token weights (for weighted mean-pooling), an
// optional token->row mapping, and the pooling parameters. It is read-only after
// loading and safe for concurrent use.
type Model struct {
	vocab    map[string]int
	matrix   []float32 // row-major [rows*dim]
	weights  []float64 // per-token weights, or nil (unweighted mean)
	mapping  []int     // token->row map, or nil (identity)
	rows     int
	dim      int
	unkID    int
	maxChars int
}

// Dim returns the embedding dimensionality.
func (m *Model) Dim() int { return m.dim }

// EmbedModelID reports the stable identity used to key stored vectors.
func (m *Model) EmbedModelID() string { return ModelName }

// Embed returns one L2-normalized vector per input text.
func (m *Model) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = m.embedOne(t)
	}
	return out, nil
}

// embedOne tokenizes text, pools the token vectors (a weighted mean when the
// model carries per-token weights, else a plain mean), and L2-normalizes the
// result (matching the potion model's normalize=true). Accumulation is float64
// to match the reference. Empty or fully-filtered text yields a zero vector.
func (m *Model) embedOne(text string) []float32 {
	ids := m.tokenizeIDs(text)
	out := make([]float32, m.dim)
	if len(ids) == 0 {
		return out
	}
	acc := make([]float64, m.dim)
	var wsum float64
	for _, id := range ids {
		if id < 0 || id >= m.rows {
			continue
		}
		row := id
		if m.mapping != nil {
			row = m.mapping[id]
		}
		w := 1.0
		if m.weights != nil {
			w = m.weights[id]
		}
		off := row * m.dim
		for j := range m.dim {
			acc[j] += w * float64(m.matrix[off+j])
		}
		wsum += w
	}
	if wsum == 0 {
		return out
	}
	var sum float64
	for j := range acc {
		acc[j] /= wsum
		sum += acc[j] * acc[j]
	}
	inv := 1.0
	if sum > 0 {
		inv = 1 / math.Sqrt(sum)
	}
	for j := range acc {
		out[j] = float32(acc[j] * inv)
	}
	return out
}

// loadModel builds a Model from safetensors bytes and HuggingFace tokenizer.json
// bytes.
func loadModel(matrixBytes, tokenizerBytes []byte) (*Model, error) {
	st, err := loadSafetensors(matrixBytes)
	if err != nil {
		return nil, err
	}
	vocab, unkID, maxChars, err := loadTokenizer(tokenizerBytes)
	if err != nil {
		return nil, err
	}
	if len(vocab) != st.rows {
		return nil, fmt.Errorf("embed: vocab size %d != matrix rows %d", len(vocab), st.rows)
	}
	return &Model{
		vocab:    vocab,
		matrix:   st.matrix,
		weights:  st.weights,
		mapping:  st.mapping,
		rows:     st.rows,
		dim:      st.dim,
		unkID:    unkID,
		maxChars: maxChars,
	}, nil
}

// loadTokenizer extracts the WordPiece vocabulary and parameters from HuggingFace
// tokenizer.json bytes.
func loadTokenizer(raw []byte) (vocab map[string]int, unkID, maxChars int, err error) {
	const path = "tokenizer"
	var doc struct {
		Model struct {
			Vocab             map[string]int `json:"vocab"`
			UnkToken          string         `json:"unk_token"`
			MaxInputCharsWord *int           `json:"max_input_chars_per_word"`
		} `json:"model"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, 0, 0, fmt.Errorf("tokenizer %s: %w", path, err)
	}
	if len(doc.Model.Vocab) == 0 {
		return nil, 0, 0, fmt.Errorf("tokenizer %s: empty vocab", path)
	}
	unk := doc.Model.UnkToken
	if unk == "" {
		unk = "[UNK]"
	}
	id, ok := doc.Model.Vocab[unk]
	if !ok {
		return nil, 0, 0, fmt.Errorf("tokenizer %s: unk token %q not in vocab", path, unk)
	}
	maxChars = 100
	if doc.Model.MaxInputCharsWord != nil && *doc.Model.MaxInputCharsWord > 0 {
		maxChars = *doc.Model.MaxInputCharsWord
	}
	return doc.Model.Vocab, id, maxChars, nil
}
