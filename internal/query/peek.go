package query

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PeekMaxLines bounds how many lines a single peek returns, mirroring
// DefinitionMaxLines: a citation is a pointer, not a licence to dump a file.
const PeekMaxLines = 200

// maxPeekBytes caps the bytes read before slicing, so peeking a pathological
// file (a giant generated blob) cannot blow memory.
const maxPeekBytes = 4 << 20

// Peek is a bounded, cited line range of a file. It turns a citation an agent
// already has (a search or references hit at file:line) into the actual code,
// without a whole-file read and without leaving the CLI.
type Peek struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// PeekLines reads lines [start,end] (1-indexed, inclusive) of rel under root.
// The read is rooted (traversal outside root is rejected), the byte read is
// capped, and the returned span is clamped to the file and to PeekMaxLines.
func PeekLines(root, rel string, start, end int) (Peek, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return Peek{}, fmt.Errorf("invalid path %q", rel)
	}
	rooted, err := os.OpenRoot(root)
	if err != nil {
		return Peek{}, err
	}
	defer rooted.Close()
	f, err := rooted.Open(clean)
	if err != nil {
		return Peek{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPeekBytes))
	if err != nil {
		return Peek{}, err
	}
	lines := strings.Split(string(data), "\n")
	// A trailing newline yields a final empty element; drop it so line counts
	// match an editor's.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	total := len(lines)
	if start < 1 {
		start = 1
	}
	if start > total {
		start = total
	}
	if end <= 0 || end > total {
		end = total
	}
	if end < start {
		end = start
	}
	truncated := false
	if end-start+1 > PeekMaxLines {
		end = start + PeekMaxLines - 1
		truncated = true
	}
	text := ""
	if total > 0 {
		text = strings.Join(lines[start-1:end], "\n")
	}
	return Peek{File: filepath.ToSlash(clean), StartLine: start, EndLine: end, Text: text, Truncated: truncated}, nil
}
