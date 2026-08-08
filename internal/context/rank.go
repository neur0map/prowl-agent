package context

import (
	"sort"
	"strconv"
)

// Candidate is an unpacked retrieval result with progressive detail variants.
type Candidate struct {
	Item
	CompactContent  string
	StandardContent string
	FullContent     string
	LexicalScore    float64
	SemanticScore   float64
	DirectMatch     bool
	Knowledge       bool
	GraphDistance   int
	ChangedRelated  bool
	LowSignal       bool
	LowSignalClass  string
	SymbolMatch     bool
}

// SemanticReranker optionally assigns normalized relevance scores. Implementations
// may use local embeddings or a host provider; errors always preserve deterministic ranking.
type SemanticReranker interface {
	Scores(question string, candidates []Candidate) (map[string]float64, error)
}

func applySemanticScores(question string, candidates []Candidate, reranker SemanticReranker) []Candidate {
	out := append([]Candidate(nil), candidates...)
	if reranker == nil || len(out) == 0 {
		return out
	}
	scores, err := reranker.Scores(question, append([]Candidate(nil), out...))
	if err != nil {
		return out
	}
	for index := range out {
		score, ok := scores[out[index].ID]
		if !ok {
			continue
		}
		if score < 0 {
			score = 0
		} else if score > 1 {
			score = 1
		}
		out[index].SemanticScore = score
	}
	return out
}

// RankCandidates applies explicit deterministic factors and records every
// positive/negative reason instead of exposing only a synthetic score.
func RankCandidates(candidates []Candidate) []Candidate {
	type scored struct {
		candidate Candidate
		score     float64
	}
	ranked := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		score := candidate.LexicalScore
		reasons := append([]string(nil), candidate.WhySelected...)
		if candidate.LowSignal && !candidate.DirectMatch {
			factor := lowSignalDampening
			if candidate.LowSignalClass == "docs" {
				factor = docDampening
			}
			score *= factor
			reasons = append(reasons, candidate.LowSignalClass+" file, low-signal (down-weighted)")
		}
		if candidate.SemanticScore > 0 {
			score += candidate.SemanticScore * 12
			reasons = append(reasons, "semantic relevance score "+formatScore(candidate.SemanticScore))
		}
		if candidate.DirectMatch {
			score += 100
			reasons = append(reasons, "direct ID match")
		}
		if candidate.Knowledge {
			score += 15
			reasons = append(reasons, "curated knowledge")
		}
		if candidate.Freshness == "current" {
			score += 10
			reasons = append(reasons, "evidence is current")
		} else if candidate.Freshness == "stale" {
			score -= 20
			reasons = append(reasons, "knowledge evidence is stale")
		}
		score += candidate.Confidence * 5
		if candidate.GraphDistance > 0 {
			score -= float64(candidate.GraphDistance * 2)
			reasons = append(reasons, "related at graph distance "+itoa(candidate.GraphDistance))
		}
		if candidate.ChangedRelated {
			score += 8
			reasons = append(reasons, "related to a changed file")
		}
		if candidate.SymbolMatch && !candidate.DirectMatch {
			score += symbolMatchBoost
			reasons = append(reasons, "defines a symbol matching the query")
		}
		candidate.WhySelected = uniqueStrings(reasons)
		ranked = append(ranked, scored{candidate: candidate, score: score})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].candidate.ID < ranked[j].candidate.ID
		}
		return ranked[i].score > ranked[j].score
	})
	out := make([]Candidate, len(ranked))
	for index := range ranked {
		out[index] = ranked[index].candidate
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func formatScore(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
