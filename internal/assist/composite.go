package assist

import "context"

// Embedder produces vectors for texts -- the embedding half of the assist stack,
// satisfied in-process by the static model (internal/embed) or remotely by an
// Ollama model.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Composite serves embeddings from Emb and generation/rerank from Assist. Assist
// may be nil: rewrite becomes a no-op and rerank keeps the input order, so vector
// + FTS search still works with no local LLM or coding agent. It exists so the
// always-available in-process embedder can pair with an optional coding-agent CLI
// for the generate/rerank half.
type Composite struct {
	Emb    Embedder
	Assist Inferencer
}

var _ Inferencer = Composite{}

// Embed delegates to the embedder.
func (c Composite) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return c.Emb.Embed(ctx, texts)
}

// Generate delegates to the assist model, or returns "" (no rewrite) when none.
func (c Composite) Generate(ctx context.Context, prompt string) (string, error) {
	if c.Assist == nil {
		return "", nil
	}
	return c.Assist.Generate(ctx, prompt)
}

// Rerank delegates to the assist model, or returns the identity order when none.
func (c Composite) Rerank(ctx context.Context, query string, docs []string) ([]int, error) {
	if c.Assist == nil {
		order := make([]int, len(docs))
		for i := range order {
			order[i] = i
		}
		return order, nil
	}
	return c.Assist.Rerank(ctx, query, docs)
}

// SupportsEmbeddings reports true so the indexer builds vectors with this backend.
func (c Composite) SupportsEmbeddings() bool { return true }

// EmbedModelID delegates to the embedder so stored vectors are keyed by the model
// that produced them (triggering a re-embed when the backend changes).
func (c Composite) EmbedModelID() string {
	if id, ok := c.Emb.(interface{ EmbedModelID() string }); ok {
		return id.EmbedModelID()
	}
	return ""
}
