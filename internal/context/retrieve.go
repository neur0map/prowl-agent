package context

import (
	"encoding/base64"
	"path/filepath"
	"sort"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

func knowledgeCandidates(repo *knowledge.Repository, sourceRoot, query string) ([]Candidate, error) {
	if repo == nil {
		return nil, nil
	}
	docs, err := repo.List()
	if err != nil {
		return nil, err
	}
	terms := queryTerms(query)
	var out []Candidate
	for _, doc := range docs {
		score := lexicalScore(terms, doc.Title+" "+doc.Description+" "+strings.Join(doc.Tags, " ")+" "+string(doc.Body))
		if len(terms) > 0 && score == 0 {
			continue
		}
		freshness := "unverified"
		citations := make([]Citation, 0, len(doc.Prowl.Anchors)+1)
		if doc.Resource != "" {
			citations = append(citations, Citation{URI: doc.Resource})
		}
		if len(doc.Prowl.Anchors) > 0 {
			freshness = "current"
			for _, anchor := range doc.Prowl.Anchors {
				check := knowledge.CheckAnchor(sourceRoot, anchor)
				if check.Status != knowledge.AnchorCurrent {
					freshness = string(check.Status)
				}
				citations = append(citations, Citation{URI: "file://" + filepath.ToSlash(anchor.Path), Path: filepath.ToSlash(anchor.Path), LineStart: anchor.LineStart, LineEnd: anchor.LineEnd, ContentHash: anchor.ContentHash})
			}
		}
		id := doc.Prowl.ID
		if id == "" {
			id = strings.TrimSuffix(filepath.ToSlash(doc.Path), filepath.Ext(doc.Path))
		}
		out = append(out, Candidate{
			Item: Item{
				ID: "concept:" + id, Kind: "knowledge:" + doc.Type, Title: doc.Title,
				Summary: doc.Description, WhySelected: []string{"lexical knowledge match"},
				Freshness: freshness, Confidence: confidenceValue(doc.Prowl.Confidence),
				Audience: []string{"assistant", "user"}, Citations: citations,
				DetailResource: "prowl://workspace/current/concept/" + id,
			},
			CompactContent: doc.Description, StandardContent: firstParagraph(doc.Body), FullContent: string(doc.Body),
			LexicalScore: score, Knowledge: true,
		})
	}
	return out, nil
}

func sourceCandidates(target *store.Store, query string, limit int) ([]Candidate, error) {
	if target == nil || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	hits, err := target.SearchChunkText(query, limit)
	if err != nil {
		return nil, err
	}
	terms := queryTerms(query)
	out := make([]Candidate, 0, len(hits))
	for _, hit := range hits {
		end := hit.StartLine + strings.Count(hit.Text, "\n")
		idPayload := filepath.ToSlash(hit.File) + ":" + itoa(hit.StartLine)
		id := "source:" + base64.RawURLEncoding.EncodeToString([]byte(idPayload))
		out = append(out, Candidate{
			Item: Item{
				ID: id, Kind: "source", Title: filepath.ToSlash(hit.File), Summary: firstParagraph([]byte(hit.Text)),
				WhySelected: []string{"full-text source match"}, Freshness: "current", Confidence: 1,
				Audience:       []string{"assistant", "user"},
				Citations:      []Citation{{URI: "file://" + filepath.ToSlash(hit.File), Path: filepath.ToSlash(hit.File), LineStart: hit.StartLine, LineEnd: end}},
				DetailResource: "prowl://workspace/current/source/" + base64.RawURLEncoding.EncodeToString([]byte(filepath.ToSlash(hit.File))),
			},
			CompactContent: firstParagraph([]byte(hit.Text)), StandardContent: hit.Text, FullContent: hit.Text,
			LexicalScore: lexicalScore(terms, hit.Text),
		})
	}
	return out, nil
}

func queryTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	seen := map[string]bool{}
	var terms []string
	for _, field := range fields {
		field = strings.Trim(field, ".,:;!?()[]{}\"'`")
		if len(field) > 1 && !seen[field] {
			seen[field] = true
			terms = append(terms, field)
		}
	}
	sort.Strings(terms)
	return terms
}

func lexicalScore(terms []string, text string) float64 {
	text = strings.ToLower(text)
	var score float64
	for _, term := range terms {
		count := strings.Count(text, term)
		if count > 3 {
			count = 3
		}
		score += float64(count * 5)
	}
	return score
}

func confidenceValue(value string) float64 {
	switch strings.ToLower(value) {
	case "verified", "high":
		return 1
	case "medium", "likely":
		return 0.7
	case "low", "uncertain":
		return 0.4
	default:
		return 0.8
	}
}

func firstParagraph(body []byte) string {
	text := strings.TrimSpace(string(body))
	if index := strings.Index(text, "\n\n"); index >= 0 {
		text = text[:index]
	}
	if len(text) > 500 {
		text = text[:500] + "…"
	}
	return text
}
