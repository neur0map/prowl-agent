package context

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRequestValidationAndEmptyCollections(t *testing.T) {
	request := Request{Mode: ModeFull}
	if err := request.Validate(); err == nil {
		t.Fatal("unbounded full request accepted")
	}
	request = Request{Mode: "future", BudgetTokens: 10}
	if err := request.Validate(); err == nil {
		t.Fatal("unknown mode accepted")
	}
	request = Request{}
	if err := request.Validate(); err != nil || request.Mode != ModeCompact || request.IDs == nil || request.Filters == nil {
		t.Fatalf("default request = %+v, %v", request, err)
	}
	packet := emptyPacket(request)
	encoded, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"items":null`) || strings.Contains(string(encoded), `"next":null`) || strings.Contains(string(encoded), `"omitted":null`) {
		t.Fatalf("ambiguous null collections: %s", encoded)
	}
}

func TestRankCandidatesExplainsFreshnessAndUsesDeterministicTies(t *testing.T) {
	candidates := []Candidate{
		{Item: Item{ID: "knowledge", Freshness: "stale", Confidence: 1}, Knowledge: true, LexicalScore: 10},
		{Item: Item{ID: "source-b", Freshness: "current", Confidence: 1}, LexicalScore: 10},
		{Item: Item{ID: "source-a", Freshness: "current", Confidence: 1}, LexicalScore: 10},
	}
	ranked := RankCandidates(candidates)
	if ranked[0].ID != "source-a" || ranked[1].ID != "source-b" || ranked[2].ID != "knowledge" {
		t.Fatalf("ranking = %+v", ranked)
	}
	if !containsReason(ranked[2].WhySelected, "knowledge evidence is stale") {
		t.Fatalf("staleness not explained: %+v", ranked[2])
	}
}

func TestPackHonorsExactBudgetAndReportsOmissions(t *testing.T) {
	request := Request{Mode: ModeCompact, BudgetBytes: 7}
	candidates := []Candidate{
		{Item: Item{ID: "a", Title: "A", Freshness: "current", Confidence: 1, DetailResource: "prowl://a"}, CompactContent: "1234", LexicalScore: 2},
		{Item: Item{ID: "b", Title: "B", Freshness: "current", Confidence: 1, DetailResource: "prowl://b"}, CompactContent: "5678", LexicalScore: 1},
	}
	packet, err := Pack(request, candidates, ByteQuarterEstimator{})
	if err != nil {
		t.Fatal(err)
	}
	if packet.SchemaVersion != PacketSchemaVersion || len(packet.Items) != 1 || packet.Items[0].ID != "a" || packet.Budget.EstimatedBytes != 7 || packet.Budget.ExactBytes != 7 || packet.Omitted["budget"] != 1 {
		t.Fatalf("packet = %+v", packet)
	}
	if len(packet.Next) == 0 || len(packet.Items[0].WhySelected) == 0 {
		t.Fatalf("omission guidance or selection reason missing: %+v", packet)
	}
}

func TestPackFallsBackToCompactAndDiversifiesSources(t *testing.T) {
	request := Request{Mode: ModeStandard, BudgetBytes: 30}
	candidates := []Candidate{
		{Item: Item{ID: "a1", Title: "A", Freshness: "current", Confidence: 1, Citations: []Citation{{Path: "a.go"}}}, CompactContent: "small", StandardContent: strings.Repeat("x", 100), LexicalScore: 10},
		{Item: Item{ID: "a2", Title: "A2", Freshness: "current", Confidence: 1, Citations: []Citation{{Path: "a.go"}}}, CompactContent: "small", LexicalScore: 9},
		{Item: Item{ID: "b1", Title: "B", Freshness: "current", Confidence: 1, Citations: []Citation{{Path: "b.go"}}}, CompactContent: "small", LexicalScore: 8},
	}
	packet, err := Pack(request, candidates, ByteQuarterEstimator{})
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Items) < 2 || packet.Items[0].ID != "a1" || packet.Items[1].ID != "b1" {
		t.Fatalf("diversity ordering = %+v", packet.Items)
	}
	if !containsReason(packet.Items[0].WhySelected, "packed as compact") {
		t.Fatalf("fallback not disclosed: %+v", packet.Items[0])
	}
}

func TestSemanticRerankingIsOptionalAndFailurePreservesDeterminism(t *testing.T) {
	candidates := []Candidate{
		{Item: Item{ID: "a"}, LexicalScore: 1},
		{Item: Item{ID: "b"}, LexicalScore: 1},
	}
	boosted := applySemanticScores("question", candidates, fakeSemanticReranker{scores: map[string]float64{"b": 1}})
	if ranked := RankCandidates(boosted); ranked[0].ID != "b" {
		t.Fatalf("semantic ranking = %+v", ranked)
	}
	fallback := applySemanticScores("question", candidates, fakeSemanticReranker{err: fmt.Errorf("provider unavailable")})
	if ranked := RankCandidates(fallback); ranked[0].ID != "a" {
		t.Fatalf("fallback ranking = %+v", ranked)
	}
}

func TestCitationEndLineUsesInclusiveChunkRange(t *testing.T) {
	for _, test := range []struct {
		text string
		want int
	}{
		{"one line\n", 10},
		{"one\ntwo\n", 11},
		{"one\ntwo", 11},
		{"", 10},
	} {
		if got := citationEndLine(10, test.text); got != test.want {
			t.Fatalf("citationEndLine(10, %q) = %d, want %d", test.text, got, test.want)
		}
	}
}

type fakeSemanticReranker struct {
	scores map[string]float64
	err    error
}

func (reranker fakeSemanticReranker) Scores(_ string, _ []Candidate) (map[string]float64, error) {
	return reranker.scores, reranker.err
}

func containsReason(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
