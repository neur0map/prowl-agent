package query

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// Definition is a symbol's cited source: its location, signature, and body,
// bounded so a large symbol cannot blow the budget. It lets an agent read one
// function or component instead of the whole file, which is prowl's core promise
// applied to the find-then-read step: locate a symbol, then read only it.
type Definition struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Signature string `json:"signature,omitempty"`
	Code      string `json:"code"`
	Truncated bool   `json:"truncated,omitempty"`
}

// DefinitionMaxLines bounds the body a single Definition returns; a larger
// symbol is truncated with Truncated set, and LineEnd still reports its real
// end so the caller knows how much remains.
const DefinitionMaxLines = 200

// Definition resolves a symbol by numeric id (from a find result) or by name
// (best-ranked match) and returns its cited source, read from root and bounded
// to DefinitionMaxLines. Read-only; the body reflects the current file.
func (q *Querier) Definition(root, target string) (Definition, error) {
	release, err := q.beginRead(context.Background())
	if err != nil {
		return Definition{}, err
	}
	defer release()

	target = strings.TrimSpace(target)
	if target == "" {
		return Definition{}, fmt.Errorf("a symbol name or id is required")
	}

	var hit store.SymbolHit
	if id, convErr := strconv.ParseInt(target, 10, 64); convErr == nil {
		h, ok, err := q.s.SymbolByID(id)
		if err != nil {
			return Definition{}, err
		}
		if !ok {
			return Definition{}, fmt.Errorf("no symbol with id %d", id)
		}
		hit = h
	} else {
		hits, err := q.findSymbolLocked(target)
		if err != nil {
			return Definition{}, err
		}
		if len(hits) == 0 {
			return Definition{}, fmt.Errorf("no symbol named %q (try prowl-agent find)", target)
		}
		hit = hits[0]
	}

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(hit.File)))
	if err != nil {
		return Definition{}, fmt.Errorf("read %s: %w", hit.File, err)
	}
	lines := strings.Split(string(data), "\n")
	start := hit.Line
	if start < 1 {
		start = 1
	}
	end := hit.EndLine
	if end < start {
		end = start
	}
	// A QML component symbol is filename-derived with a placeholder 1-1 range;
	// its real definition is the whole file, so read to the end.
	if hit.Kind == "component" && end <= 1 {
		end = len(lines)
	}
	if end > len(lines) {
		end = len(lines)
	}
	start = docCommentStart(lines, start)
	body := lines[start-1 : end]
	truncated := false
	if len(body) > DefinitionMaxLines {
		body = body[:DefinitionMaxLines]
		truncated = true
	}
	return Definition{
		Name:      hit.Name,
		Kind:      hit.Kind,
		File:      hit.File,
		LineStart: start,
		LineEnd:   end,
		Signature: hit.Signature,
		Code:      strings.Join(body, "\n"),
		Truncated: truncated,
	}, nil
}

// isCommentLine reports whether a trimmed line begins with a comment marker
// common across the indexed languages (// # /* * -- <!--). It is a heuristic,
// used only to attach a symbol's own doc block, never to parse code.
func isCommentLine(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	for _, p := range []string{"//", "#", "/*", "*", "--", "<!--"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// docCommentStart walks up from a symbol's first line to include a contiguous
// block of comment lines directly above it (its doc comment), stopping at the
// first blank or non-comment line. It is bounded so a file-top license header
// separated by a blank line is never mistaken for a symbol's doc.
func docCommentStart(lines []string, start int) int {
	const maxDoc = 15
	s := start
	for s > 1 && start-s < maxDoc && isCommentLine(lines[s-2]) {
		s--
	}
	return s
}
