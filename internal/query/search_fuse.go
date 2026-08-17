package query

import (
	"os"
	"sort"
	"strconv"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// docChunks returns doc-comment chunk matches for fusion, swallowing any error:
// doc matching is an additive recall signal, so failing to consult it must never
// fail an otherwise-good search. A nil slice fuses as a no-op.
func (q *Querier) docChunks(text string, k int) []store.ChunkHit {
	hits, err := q.s.ChunksByDocMatch(text, k)
	if err != nil {
		return nil
	}
	return hits
}

// weightedHits is one ranked list entering the fusion, with the weight its
// reciprocal ranks carry. Vector and lexical signals rank code and enter at full
// weight; the doc signal ranks the prose that describes code and enters
// down-weighted (docFuseWeight).
type weightedHits struct {
	hits   []store.ChunkHit
	weight float64
}

// docFuseWeight is the weight of the doc signal relative to a primary (vector or
// lexical) signal, derived from what the doc signal is FOR, not fitted to any
// query. Its job is recall: surfacing a chunk whose doc comment answers the query
// when the chunk's code carries none of the query terms (the redact.py case). It
// is not a ranking signal -- two independent cross-encoders that ranked prose
// about a concept as if it were the code implementing the concept both made this
// corpus's retrieval worse, and a full-weight (1.0) doc list reproduces that
// exact failure: it floated test_hermes_logging.py above source and demoted four
// of five control queries. So the doc signal counts as HALF of one primary
// signal: enough that the strongest doc-only match lands near the bottom of the
// result window and becomes visible, never enough to out-score a chunk two
// full-weight code signals already agree on. 0.5 is that ratio, chosen before
// measuring the guard; it is not tuned per query.
var docFuseWeight = envFloat("PROWL_DOC_FUSE_WEIGHT", 0.5)

// envFloat reads a float override from the environment (used only to characterize
// the weight/quality tradeoff during review), falling back to def when unset or
// unparseable.
func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// fuseRRF merges ranked chunk lists by weighted reciprocal rank fusion, deduped
// by file:start_line. Each list contributes weight/(k0+rank) to a chunk's score,
// so a chunk several signals agree on accumulates their contributions and rises;
// k0=60 is the standard RRF damping. A stable sort by score keeps a chunk's
// first-seen position on a tie, so lists passed earlier win ties over later ones.
//
// Where the doc signal sits, and why it cannot invert the tiering (constraint
// C5). Callers pass the tier-ordered lexical list (searchChunksRanked, which
// encodes source > tests > docs > vendored and the path-concept boost) and, in
// the hybrid path, the vector list, both at full weight; the doc list
// (ChunksByDocMatch) is always passed LAST and at docFuseWeight < 1. Three
// properties keep the tiering dominant:
//   - The lexical list contributes its full tier-ordered reciprocal ranks to
//     every chunk it holds, and RRF only ever ADDS score, so a chunk the tiering
//     ranked well keeps that score; the doc signal can raise a chunk, never lower.
//   - The doc contribution is scaled by docFuseWeight, so a doc-only match enters
//     at a fraction of one list's reciprocal rank -- visible, but unable to
//     out-score a chunk two full-weight signals already agree on, which is what
//     stops doc prose from displacing the code that implements the concept.
//   - Passing the doc list last breaks a score tie in favour of the existing hit.
//
// The doc field indexes symbol doc comments, which live on source code, so the
// signal reinforces tier-0 "code that implements the thing"; the down-weight
// keeps it from floating the tier-1/2 test and doc files whose docstrings merely
// mention the concept -- the exact failure that made two cross-encoder rerankers
// worse on this corpus. It is fused as one more signal, never a gate or override.
func fuseRRF(limit int, lists ...weightedHits) []store.ChunkHit {
	const k0 = 60.0
	type agg struct {
		hit   store.ChunkHit
		score float64
	}
	m := map[string]*agg{}
	var order []string
	for _, list := range lists {
		for rank, h := range list.hits {
			key := h.File + ":" + strconv.Itoa(h.StartLine)
			e, ok := m[key]
			if !ok {
				e = &agg{hit: h}
				m[key] = e
				order = append(order, key)
			}
			e.score += list.weight / (k0 + float64(rank+1))
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
