package extract

import (
	"regexp"
	"strings"
)

var reHasExt = regexp.MustCompile(`\.[A-Za-z0-9]{1,6}$`)

// looksLikeLocalPath reports whether v is plausibly a local file reference: an
// absolute/home/relative path, or a slash path ending in a file extension. It
// rejects URLs, version/action refs (foo/bar@v1), and bare config namespaces
// (custom/power) so config string values are not mistaken for file references.
func looksLikeLocalPath(v string) bool {
	v = strings.TrimSpace(strings.Trim(v, `"'`))
	if v == "" || strings.Contains(v, "://") || strings.ContainsAny(v, " \t") {
		return false
	}
	if strings.HasPrefix(v, "/") || strings.HasPrefix(v, "~/") ||
		strings.HasPrefix(v, "./") || strings.HasPrefix(v, "../") {
		return true
	}
	if strings.Contains(v, "@") {
		return false
	}
	return strings.Contains(v, "/") && reHasExt.MatchString(v)
}

// valueAfterEq returns the trimmed text following the first '=' in s.
func valueAfterEq(s string) string {
	if i := strings.IndexByte(s, '='); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return ""
}

// looksLikeColor reports whether v is a color literal (#hex, rgb(), 0x..).
func looksLikeColor(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return strings.HasPrefix(v, "#") || strings.HasPrefix(v, "rgb") || strings.HasPrefix(v, "0x")
}

// looksLikePath reports whether v resembles a filesystem path.
func looksLikePath(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	return strings.HasPrefix(v, "/") || strings.HasPrefix(v, "~") ||
		strings.HasPrefix(v, "./") || strings.HasPrefix(v, "../") || strings.Contains(v, "/")
}

// classifyResource picks a resource kind from a value.
func classifyResource(v string) string {
	switch {
	case looksLikeColor(v):
		return "color"
	case looksLikePath(v):
		return "path"
	default:
		return "var"
	}
}

// splitCSV splits a comma-separated string and trims each field.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}
