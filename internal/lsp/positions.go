package lsp

import (
	"net/url"
	"path/filepath"
	"strings"
)

// uriToPath converts a file:// URI to a filesystem path.
func uriToPath(uri string) string {
	if u, err := url.Parse(uri); err == nil && u.Scheme == "file" {
		if p, err := url.PathUnescape(u.Path); err == nil {
			return p
		}
		return u.Path
	}
	return strings.TrimPrefix(uri, "file://")
}

// pathToURI converts a filesystem path to a file:// URI.
func pathToURI(p string) string {
	u := url.URL{Scheme: "file", Path: p}
	return u.String()
}

// relFromURI returns the slash path of uri relative to root, and whether it is
// inside root.
func relFromURI(root, uri string) (string, bool) {
	rel, err := filepath.Rel(root, uriToPath(uri))
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// uriForRel builds a file:// URI for a project-relative slash path.
func uriForRel(root, rel string) string {
	return pathToURI(filepath.Join(root, filepath.FromSlash(rel)))
}

// lineText returns the zero-based line of text, or "" if out of range.
func lineText(text string, line int) string {
	if line < 0 {
		return ""
	}
	start := 0
	cur := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			if cur == line {
				return strings.TrimSuffix(text[start:i], "\r")
			}
			cur++
			start = i + 1
		}
	}
	if cur == line {
		return strings.TrimSuffix(text[start:], "\r")
	}
	return ""
}

// isTokenByte reports whether b can be part of a config token (names, paths,
// variables: letters, digits, and the punctuation rices use in identifiers).
func isTokenByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '_', '-', '.', '/', '$', '@', '~':
		return true
	}
	return false
}

// tokenAt returns the token surrounding character ch on the given line, with its
// start and end columns. ch is clamped into range.
func tokenAt(line string, ch int) (tok string, start, end int) {
	if ch > len(line) {
		ch = len(line)
	}
	if ch < 0 {
		ch = 0
	}
	start = ch
	for start > 0 && isTokenByte(line[start-1]) {
		start--
	}
	end = ch
	for end < len(line) && isTokenByte(line[end]) {
		end++
	}
	return line[start:end], start, end
}

// wordPrefix returns the token text immediately before character ch (for
// completion), and its start column.
func wordPrefix(line string, ch int) (string, int) {
	if ch > len(line) {
		ch = len(line)
	}
	start := ch
	for start > 0 && isTokenByte(line[start-1]) {
		start--
	}
	return line[start:ch], start
}
