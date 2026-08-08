package query

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// OutlineSymbol is one entry in a file's structural skeleton: a symbol's kind,
// name, signature, and line range, with Depth giving its nesting under enclosing
// symbols (a method inside a class has depth 1). No body is included.
type OutlineSymbol struct {
	Depth     int    `json:"depth"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
}

// FileOutline is a token-efficient skeletal view of one file: every symbol it
// defines, in source order, nested by line-range containment. It lets an agent
// grasp a file's shape from a handful of signature lines instead of reading the
// whole file, which is prowl's core promise applied to file-level structure.
type FileOutline struct {
	File    string          `json:"file"`
	Symbols []OutlineSymbol `json:"symbols"`
}

// Outline returns a file's structural skeleton from the index alone: no file
// read, no bodies. Nesting depth is derived deterministically from line-range
// containment, so it is language-agnostic. The path is repo-relative, like
// impact and callers.
func (q *Querier) Outline(path string) (FileOutline, error) {
	release, err := q.beginRead(context.Background())
	if err != nil {
		return FileOutline{}, err
	}
	defer release()
	path = strings.TrimSpace(path)
	if path == "" {
		return FileOutline{}, fmt.Errorf("a file path is required")
	}
	rel := filepath.ToSlash(path)
	fileID, err := q.s.FileID(rel)
	if err != nil {
		return FileOutline{}, fmt.Errorf("file not indexed: %s", path)
	}
	syms, err := q.s.SymbolsInFile(fileID)
	if err != nil {
		return FileOutline{}, err
	}
	out := FileOutline{File: rel, Symbols: make([]OutlineSymbol, 0, len(syms))}
	var stack []store.SymbolHit
	for _, s := range syms {
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			if top.Line < s.Line && s.EndLine <= top.EndLine {
				break // top encloses s: s nests under it
			}
			stack = stack[:len(stack)-1]
		}
		out.Symbols = append(out.Symbols, OutlineSymbol{
			Depth:     len(stack),
			Kind:      s.Kind,
			Name:      s.Name,
			Signature: s.Signature,
			LineStart: s.Line,
			LineEnd:   s.EndLine,
		})
		stack = append(stack, s)
	}
	return out, nil
}
