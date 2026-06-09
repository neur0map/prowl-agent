package index

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/prowl-agent/prowl-agent/internal/graph"
	"github.com/prowl-agent/prowl-agent/internal/parse"
	"github.com/prowl-agent/prowl-agent/internal/parse/extract"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

// Summary reports what an Index run did.
type Summary struct {
	Indexed int // supported files seen
	Parsed  int // files (re)parsed this run
	Skipped int // unchanged files
	Deleted int // files removed from the index
	Symbols int
	Edges   int
}

// Index incrementally synchronizes the store with the rice rooted at root.
// Only files whose content hash changed are reparsed; removed files are deleted;
// the global resolution passes always run afterward.
func Index(s *store.Store, root string, ignore []string) (Summary, error) {
	var sum Summary
	rels, err := Walk(root, ignore)
	if err != nil {
		return sum, err
	}
	current := make(map[string]bool, len(rels))

	for _, rel := range rels {
		full := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		head := data
		if len(head) > 512 {
			head = head[:512]
		}
		lang := parse.Detect(rel, head)
		if lang == "" {
			continue
		}
		current[rel] = true
		sum.Indexed++

		hash := strconv.FormatUint(xxhash.Sum64(data), 16)
		if existing, ok, err := s.GetFileByPath(rel); err != nil {
			return sum, err
		} else if ok && existing.Hash == hash {
			sum.Skipped++
			continue
		}

		ex, ok := extract.For(lang)
		if !ok {
			continue
		}
		res, _ := ex.Extract(data) // partial extraction is acceptable; chunks still stored

		var mtime int64
		if info, err := os.Stat(full); err == nil {
			mtime = info.ModTime().Unix()
		}
		fid, err := s.UpsertFile(store.File{
			RelPath: rel, Lang: lang, Role: graph.InferRole(rel, lang),
			Size: int64(len(data)), Hash: hash, MTime: mtime,
		})
		if err != nil {
			return sum, err
		}
		syms, ress, edges, chunks := mapResult(res)
		if err := s.ReplaceFileGraph(fid, syms, ress, edges, chunks); err != nil {
			return sum, err
		}
		sum.Parsed++
		sum.Symbols += len(syms)
		sum.Edges += len(edges)
	}

	// Remove files that disappeared from the rice.
	all, err := s.AllFiles()
	if err != nil {
		return sum, err
	}
	for _, f := range all {
		if !current[f.RelPath] {
			if err := s.DeleteFileByPath(f.RelPath); err != nil {
				return sum, err
			}
			sum.Deleted++
		}
	}

	if err := graph.Resolve(s); err != nil {
		return sum, err
	}
	if err := s.SetMeta("last_index", strconv.FormatInt(time.Now().Unix(), 10)); err != nil {
		return sum, err
	}
	return sum, nil
}

func mapResult(r extract.Result) ([]store.Symbol, []store.Resource, []store.RawEdge, []store.Chunk) {
	syms := make([]store.Symbol, len(r.Symbols))
	for i, s := range r.Symbols {
		syms[i] = store.Symbol{Name: s.Name, Kind: s.Kind, Signature: s.Signature, StartLine: s.StartLine, EndLine: s.EndLine, ParentName: s.Parent}
	}
	ress := make([]store.Resource, len(r.Resources))
	for i, rs := range r.Resources {
		ress[i] = store.Resource{Kind: rs.Kind, Name: rs.Name, Value: rs.Value, Line: rs.Line}
	}
	edges := make([]store.RawEdge, len(r.Edges))
	for i, e := range r.Edges {
		edges[i] = store.RawEdge{SrcName: e.SrcName, Kind: e.Kind, Raw: e.Raw, Line: e.Line}
	}
	chunks := make([]store.Chunk, len(r.Chunks))
	for i, c := range r.Chunks {
		chunks[i] = store.Chunk{StartLine: c.StartLine, EndLine: c.EndLine, Text: c.Text}
	}
	return syms, ress, edges, chunks
}
