package index

import (
	"context"
	"fmt"

	"github.com/prowl-agent/prowl-agent/internal/assist"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

const embedBatch = 32

// BuildVectors embeds every chunk that lacks a vector and stores the result.
// It is incremental: only missing chunks are embedded, so repeated calls after
// an index refresh are cheap. The vec0 table is created lazily from the first
// embedding's dimension. Returns the number of chunks embedded.
func BuildVectors(ctx context.Context, s *store.Store, inf assist.Inferencer, model string) (int, error) {
	pending, err := s.ChunksWithoutVectors()
	if err != nil {
		return 0, err
	}
	embedded := 0
	for i := 0; i < len(pending); i += embedBatch {
		end := i + embedBatch
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[i:end]
		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.Text
		}
		vecs, err := inf.Embed(ctx, texts)
		if err != nil {
			return embedded, err
		}
		if len(vecs) != len(batch) {
			return embedded, fmt.Errorf("embed returned %d vectors for %d texts", len(vecs), len(batch))
		}
		if !s.VectorsReady() {
			if len(vecs[0]) == 0 {
				return embedded, fmt.Errorf("embedding model returned empty vector")
			}
			if err := s.EnableVectors(len(vecs[0]), model); err != nil {
				return embedded, err
			}
		}
		for j, c := range batch {
			if err := s.UpsertChunkVector(c.ID, vecs[j]); err != nil {
				return embedded, err
			}
			embedded++
		}
	}
	return embedded, nil
}
