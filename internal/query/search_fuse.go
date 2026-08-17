package query

import (
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

// fuseRRF merges ranked chunk lists by reciprocal rank fusion, deduped by
// file:start_line. Each list contributes 1/(k0+rank) to a chunk's score, so a
// chunk several signals agree on accumulates their contributions and rises;
// k0=60 is the standard RRF damping. A stable sort by score keeps a chunk's
// first-seen position on a tie, so lists passed earlier win ties over later ones.
//
// Where the doc signal sits, and why it cannot invert the tiering (constraint
// C5). Callers pass the tier-ordered lexical list (searchChunksRanked, which
// encodes source > tests > docs > vendored and the path-concept boost) and, in
// the hybrid path, the vector list; the doc list (ChunksByDocMatch) is always
// passed LAST. Two properties follow:
//   - The lexical list still contributes its full tier-ordered reciprocal ranks
//     to every chunk it holds, and RRF only ever ADDS score. The doc signal can
//     therefore raise a chunk (promoting code whose docstring answers the query)
//     but can never lower one, so a chunk the tiering ranked well keeps that rank.
//   - Passing the doc list last means a doc-only match -- a chunk no lexical or
//     vector signal surfaced -- enters at a single list's reciprocal rank (the
//     same magnitude as one mid-ranked hit) and, on a score tie, sorts BEHIND the
//     existing hit rather than displacing it. It becomes visible without
//     out-ranking a chunk that scored in two or three lists.
//
// The doc field indexes symbol doc comments, which live on source code, so the
// signal reinforces tier-0 "code that implements the thing" instead of promoting
// tier-2 prose files -- the exact failure that made two cross-encoder rerankers
// worse on this corpus. It is fused as one more signal, never as a gate or an
// override of the tiering.
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
