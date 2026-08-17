package extract

import "strings"

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

// DocCommentStart walks up from a symbol's first line to include a contiguous
// block of comment lines directly above it (its doc comment), stopping at the
// first blank or non-comment line. It is bounded so a file-top license header
// separated by a blank line is never mistaken for a symbol's doc.
func DocCommentStart(lines []string, start int) int {
	const maxDoc = 15
	s := start
	for s > 1 && start-s < maxDoc && isCommentLine(lines[s-2]) {
		s--
	}
	return s
}

// DocSentence reduces a raw doc comment to its first sentence with comment
// markers removed. In a well-documented codebase this sentence is the symbol's
// contract, and it is what makes an outline decidable without opening the file.
func DocSentence(doc string) string {
	var b strings.Builder
	for _, line := range strings.Split(doc, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "///")
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimPrefix(line, "/*")
		line = strings.TrimPrefix(line, "*/")
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimPrefix(line, `"""`)
		line = strings.TrimPrefix(line, "'''")
		line = strings.TrimSuffix(line, `"""`)
		line = strings.TrimSuffix(line, "'''")
		line = strings.TrimSuffix(line, "*/")
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line == "" {
			if b.Len() > 0 {
				break // a blank line ends the summary paragraph
			}
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(line)
		if strings.HasSuffix(line, ".") {
			break
		}
	}
	s := strings.TrimSpace(b.String())
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	return s
}

// PopulateDocs fills each symbol's Doc with the contiguous comment block
// directly above it -- its leading doc comment. lines is the file split once by
// the caller and shared across every symbol: doc extraction runs per symbol on
// a large repository, so re-splitting the file for each symbol would put an
// avoidable allocation on the cold-index hot path. A Doc already set by the
// extractor (a Python docstring, which sits inside the body, not above it) is
// left untouched, since that captured contract is the better summary.
func PopulateDocs(lines []string, symbols []Symbol) {
	for i := range symbols {
		if symbols[i].Doc != "" {
			continue
		}
		start := symbols[i].StartLine
		// Nothing can sit above line 1, and a start past the file end is not a
		// real position to walk up from; both cases carry no doc.
		if start < 2 || start > len(lines)+1 {
			continue
		}
		if docStart := DocCommentStart(lines, start); docStart < start {
			symbols[i].Doc = strings.Join(lines[docStart-1:start-1], "\n")
		}
	}
}
