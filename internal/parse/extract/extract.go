// Package extract turns parsed source files into symbols, resources, raw edges,
// and text chunks. Each language has an Extractor; languages without a dedicated
// grammar fall back to the line-oriented generic extractor.
package extract

import (
	"fmt"
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/prowl-agent/prowl-agent/internal/parse"
)

// Symbol is a definition found in a file.
type Symbol struct {
	Name, Kind, Signature, Parent string
	StartLine, EndLine            int
}

// Resource is a shared value (color/font/path/var) declared or used in a file.
type Resource struct {
	Kind, Name, Value string
	Line              int
}

// RawEdge is an unresolved relationship; Raw is the literal target string
// (path/module/command/var) that the resolution passes will resolve later.
// SrcName, if it matches a symbol in the same file, makes the edge symbol-sourced.
type RawEdge struct {
	SrcName, Kind, Raw string
	Line               int
}

// Chunk is a text window used for full-text search (and future embeddings).
type Chunk struct {
	StartLine, EndLine int
	Text               string
}

// Result is everything an extractor derives from one file.
type Result struct {
	Symbols   []Symbol
	Resources []Resource
	Edges     []RawEdge
	Chunks    []Chunk
}

// Extractor derives a Result from a file's bytes.
type Extractor interface {
	Lang() string
	Extract(src []byte) (Result, error)
}

var registry = map[string]Extractor{}

func register(e Extractor) { registry[e.Lang()] = e }

// For returns the extractor for lang, falling back to the generic extractor.
func For(lang string) (Extractor, bool) {
	if e, ok := registry[lang]; ok {
		return e, true
	}
	if e, ok := registry["generic"]; ok {
		return e, true
	}
	return nil, false
}

// capture is a named capture node from a query match.
type capture struct {
	Name string
	Node sitter.Node
}

// queryEach parses src as lang, runs the scm query, and invokes fn once per
// match with that match's captures. The parse tree is created and closed here.
func queryEach(lang string, src, scm []byte, fn func(caps []capture)) error {
	lng, ok := parse.Language(lang)
	if !ok {
		return fmt.Errorf("no grammar for %q", lang)
	}
	tree, err := parse.Parse(lang, src)
	if err != nil {
		return err
	}
	defer tree.Close()
	q, err := sitter.NewQuery(lng, scm)
	if err != nil {
		return fmt.Errorf("query for %s: %w", lang, err)
	}
	qc := sitter.NewQueryCursor()
	matches := qc.Matches(q, tree.RootNode(), src)
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		caps := make([]capture, len(m.Captures))
		for i, c := range m.Captures {
			caps[i] = capture{Name: q.CaptureNameForID(c.Index), Node: c.Node}
		}
		fn(caps)
	}
	return nil
}

// capNode returns the node captured under name in this match.
func capNode(caps []capture, name string) (sitter.Node, bool) {
	for _, c := range caps {
		if c.Name == name {
			return c.Node, true
		}
	}
	return sitter.Node{}, false
}

// line is the 1-based start line of a node.
func line(n sitter.Node) int { return int(n.StartPoint().Row) + 1 }

// endLine is the 1-based end line of a node.
func endLine(n sitter.Node) int { return int(n.EndPoint().Row) + 1 }

// firstChildOfType returns the first named child of n with the given type.
func firstChildOfType(n sitter.Node, typ string) (sitter.Node, bool) {
	for i := uint32(0); i < n.NamedChildCount(); i++ {
		ch := n.NamedChild(i)
		if ch.Type() == typ {
			return ch, true
		}
	}
	return sitter.Node{}, false
}

// chunkText splits src into windows of at most window lines for FTS.
func chunkText(src []byte, window int) []Chunk {
	if window <= 0 {
		window = 40
	}
	lines := strings.Split(string(src), "\n")
	var out []Chunk
	for i := 0; i < len(lines); i += window {
		end := i + window
		if end > len(lines) {
			end = len(lines)
		}
		out = append(out, Chunk{StartLine: i + 1, EndLine: end, Text: strings.Join(lines[i:end], "\n")})
	}
	return out
}

// Sexpr returns the parse tree's S-expression for a snippet — a debugging aid
// for authoring queries against a grammar's real node names.
func Sexpr(lang string, src []byte) (string, error) {
	tree, err := parse.Parse(lang, src)
	if err != nil {
		return "", err
	}
	defer tree.Close()
	return tree.RootNode().String(), nil
}

// unquote trims surrounding whitespace, then strips surrounding quotes from a
// string literal's text (config string values often carry leading whitespace).
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
