package query

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// LangCount pairs a language with how many scoped files use it.
type LangCount struct {
	Lang  string `json:"lang"`
	Files int    `json:"files"`
}

// Brief is a bounded, cited orientation for one path or subsystem: how big it
// is, which languages it uses, the architecture guides to read, and the most
// depended-on files to read first. It is one call an agent can make (or a parent
// can hand a subagent) to start warm on a slice of the repo instead of
// re-deriving its shape by reading files. Every path listed is a real indexed
// file, so the answer is cited by construction.
type Brief struct {
	Scope     string         `json:"scope"`
	Files     int            `json:"files"`
	Languages []LangCount    `json:"languages"`
	Guides    []string       `json:"guides,omitempty"`
	KeyFiles  []store.FanRow `json:"key_files"`
}

// Brief assembles a scoped orientation. scope is a repo-relative path prefix (a
// file or directory); "" or "." briefs the whole repo. Key files are ranked by
// dependency fan-in within scope; when fan-in is sparse (a leaf subsystem), the
// largest in-scope files fill in so the answer still points somewhere useful.
func (q *Querier) Brief(scope string) (Brief, error) {
	release, err := q.beginRead(context.Background())
	if err != nil {
		return Brief{}, err
	}
	defer release()

	clean := path.Clean(strings.TrimSpace(scope))
	clean = strings.Trim(clean, "/")
	inScope := func(rel string) bool {
		if clean == "" || clean == "." {
			return true
		}
		return rel == clean || strings.HasPrefix(rel, clean+"/")
	}

	files, err := q.s.AllFiles()
	if err != nil {
		return Brief{}, err
	}
	b := Brief{Scope: clean}
	if b.Scope == "" {
		b.Scope = "."
	}
	langCounts := map[string]int{}
	var scoped []store.File
	for _, f := range files {
		if inScope(f.RelPath) {
			scoped = append(scoped, f)
			langCounts[f.Lang]++
		}
	}
	b.Files = len(scoped)
	if b.Files == 0 {
		return Brief{}, fmt.Errorf("no indexed files under %q", scope)
	}
	for lang, n := range langCounts {
		b.Languages = append(b.Languages, LangCount{Lang: lang, Files: n})
	}
	sort.Slice(b.Languages, func(i, j int) bool {
		if b.Languages[i].Files != b.Languages[j].Files {
			return b.Languages[i].Files > b.Languages[j].Files
		}
		return b.Languages[i].Lang < b.Languages[j].Lang
	})

	b.Guides = guideDocs(files)

	fan, err := q.s.FanIn(400)
	if err != nil {
		return Brief{}, err
	}
	const topKey = 10
	seen := map[string]bool{}
	for _, r := range fan {
		if inScope(r.File) && !isVendored(r.File) {
			b.KeyFiles = append(b.KeyFiles, r)
			seen[r.File] = true
			if len(b.KeyFiles) >= topKey {
				break
			}
		}
	}
	if len(b.KeyFiles) < topKey {
		sort.Slice(scoped, func(i, j int) bool { return scoped[i].Size > scoped[j].Size })
		for _, f := range scoped {
			if seen[f.RelPath] || isVendored(f.RelPath) {
				continue
			}
			b.KeyFiles = append(b.KeyFiles, store.FanRow{File: f.RelPath, In: 0})
			seen[f.RelPath] = true
			if len(b.KeyFiles) >= topKey {
				break
			}
		}
	}

	return b, nil
}
