package context

import (
	stdcontext "context"
	"errors"
	"testing"
)

type semanticTestInferencer struct {
	order []int
	err   error
}

func (inferencer semanticTestInferencer) Embed(stdcontext.Context, []string) ([][]float32, error) {
	return nil, errors.New("unused")
}
func (inferencer semanticTestInferencer) Generate(stdcontext.Context, string) (string, error) {
	return "", errors.New("unused")
}
func (inferencer semanticTestInferencer) Rerank(stdcontext.Context, string, []string) ([]int, error) {
	return inferencer.order, inferencer.err
}

func TestAssistSemanticRerankerProducesNormalizedScores(t *testing.T) {
	reranker := AssistSemanticReranker{Inferencer: semanticTestInferencer{order: []int{1, 0}}}
	scores, err := reranker.Scores("question", []Candidate{{Item: Item{ID: "first", Title: "First"}}, {Item: Item{ID: "second", Title: "Second"}}})
	if err != nil {
		t.Fatal(err)
	}
	if scores["second"] != 1 || scores["first"] != 0.5 {
		t.Fatalf("scores = %+v", scores)
	}
}

func TestAssistSemanticRerankerRejectsInvalidProviderOrder(t *testing.T) {
	reranker := AssistSemanticReranker{Inferencer: semanticTestInferencer{order: []int{2}}}
	if _, err := reranker.Scores("question", []Candidate{{Item: Item{ID: "first"}}}); err == nil {
		t.Fatal("invalid provider order accepted")
	}
}
