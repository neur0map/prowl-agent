package query

import (
	"sort"
	"strconv"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// docChunks returns doc-comment chunk matches for the recall pass, swallowing any
// error: doc matching is an additive recall signal, so failing to consult it must
// never fail an otherwise-good search. A nil slice merges as a no-op.
func (q *Querier) docChunks(text string, k int) []store.ChunkHit {
	hits, err := q.s.ChunksByDocMatch(text, k)
	if err != nil {
		return nil
	}
	return hits
}

// fuseRRF merges the co-equal ranking signals -- vector KNN and the tier-ordered
// lexical list -- by reciprocal rank fusion, deduped by file:start_line. Each
// list contributes 1/(k0+rank) to a chunk's score, so a chunk both signals agree
// on accumulates their contributions and rises; k0=60 is the standard RRF
// damping. A stable sort by score keeps a chunk's first-seen position on a tie,
// so lists passed earlier win ties over later ones (vector before lexical).
//
// The doc signal is deliberately NOT a fusion input here. It was measured on the
// hermes corpus that fusing it as a third RRF list -- at any weight -- cannot
// satisfy the acceptance bar: at full weight it floods the top and floats test
// files above source (the exact cross-encoder failure C5 forbids), and
// down-weighted enough to protect the ranking it drops out of the window the
// doc-only answer it exists to surface. So the co-equal code signals are fused
// here, and the doc signal is applied afterwards by mergeDocRecall as recall
// only, where it cannot reorder this result.
func fuseRRF(limit int, lists ...[]store.ChunkHit) []store.ChunkHit {
	const k0 = 60.0
	type agg struct {
		hit   store.ChunkHit
		score float64
	}
	m := map[string]*agg{}
	var order []string
	for _, list := range lists {
		for rank, h := range list {
			key := h.File + ":" + strconv.Itoa(h.StartLine)
			e, ok := m[key]
			if !ok {
				e = &agg{hit: h}
				m[key] = e
				order = append(order, key)
			}
			e.score += 1.0 / (k0 + float64(rank+1))
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return m[order[i]].score > m[order[j]].score })
	out := make([]store.ChunkHit, 0, limit)
	for _, k := range order {
		if len(out) >= limit {
			break
		}
		out = append(out, m[k].hit)
	}
	return out
}

// docRecallShare splits the result window between the two channels: the confident
// code-based ranking (fuseRRF over vector + lexical) keeps the top
// (share-1)/share of the slots, and the doc recall channel owns the bottom
// 1/share. It is a first-principles minority split -- the doc signal is one
// recall channel against two ranking channels, so it gets a small, fixed budget
// -- not a constant tuned to make any particular query pass.
const docRecallShare = 10

// mergeDocRecall adds doc-answering chunks the base ranking missed, and does so
// WITHOUT reordering the base (constraint C5). The doc signal's only job is
// recall: surfacing a chunk whose doc comment answers the query when the chunk's
// code carries none of the query terms and neither the vector nor lexical signal
// found it -- the redact.py case, buried below fifty results because its chunk is
// 22% docstring and 78% imports. Reordering the confident matches to make room
// for prose that merely describes a concept is precisely what made two
// cross-encoder rerankers worse on this corpus, so the doc signal is forbidden
// from moving any base hit at all.
//
// Base order is preserved exactly. Doc-only misses (doc hits no base signal
// surfaced, in bm25(fts_docs) order) fill the slots the base left empty for free;
// when the base already fills the window, they claim only the reserved tail
// budget (docRecallShare), displacing just the weakest base-tail matches and
// never a confident one. So a guard query whose answer the base already ranks in
// its top slots cannot regress, and a doc-only answer the base missed still
// becomes visible in the tail.
func mergeDocRecall(base, docHits []store.ChunkHit, limit int) []store.ChunkHit {
	key := func(h store.ChunkHit) string { return h.File + ":" + strconv.Itoa(h.StartLine) }
	seen := make(map[string]bool, len(base))
	out := make([]store.ChunkHit, 0, limit)
	for _, h := range base {
		if k := key(h); !seen[k] {
			seen[k] = true
			out = append(out, h)
		}
	}
	var misses []store.ChunkHit
	for _, h := range docHits {
		if k := key(h); !seen[k] {
			seen[k] = true
			misses = append(misses, h)
		}
	}
	// Respect the base tiering inside the recall tail too: a source file whose doc
	// comment answers the query is higher-value recall than a test or doc file that
	// merely mentions the concept, so source misses lead, then tests, then
	// docs/vendored -- the same order searchChunksRanked imposes. Stable, so
	// bm25(fts_docs) order is kept within each tier.
	sort.SliceStable(misses, func(i, j int) bool {
		return searchTier(misses[i].File) < searchTier(misses[j].File)
	})
	if len(misses) == 0 {
		if len(out) > limit {
			out = out[:limit]
		}
		return out
	}
	if len(out)+len(misses) <= limit {
		return append(out, misses...)
	}
	budget := limit / docRecallShare
	if budget < 1 {
		budget = 1
	}
	if budget > len(misses) {
		budget = len(misses)
	}
	keepBase := limit - budget
	if keepBase > len(out) {
		keepBase = len(out)
	}
	res := make([]store.ChunkHit, 0, limit)
	res = append(res, out[:keepBase]...)
	res = append(res, misses[:limit-keepBase]...)
	return res
}
