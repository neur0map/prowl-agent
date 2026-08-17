// Package redact masks secret-shaped values before they are stored. prowl indexes
// whatever a repository contains, including credentials committed in source, and
// every retrieval path emits stored text into an agent's context. Masking at index
// time rather than on output is deliberate: an output filter still leaves the
// on-disk index a cleartext secret store.
//
// The identifier and structure around a value are preserved, so "where is the
// stripe token set" still resolves; only the value itself is destroyed.
package redact

import (
	"math"
	"regexp"
	"strings"
)

// Mask replaces a detected secret value.
const Mask = "[redacted]"

// provider matches credentials whose shape is unambiguous: a vendor prefix, an
// AWS key id, or a JWT. These need no entropy check.
var provider = regexp.MustCompile(
	`(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{16,}` +
		`|\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}` +
		`|gh[pousr]_[A-Za-z0-9]{20,}` +
		`|xox[abposr]-[A-Za-z0-9-]{10,}` +
		`|AKIA[0-9A-Z]{16}` +
		`|AIza[0-9A-Za-z_-]{35}` +
		`|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)

// pemBody matches the base64 payload inside a private key block, either paired
// (begin/end) or unpaired (begin to end of input).
var pemBody = regexp.MustCompile(`(?s)(-----BEGIN [A-Z ]*PRIVATE KEY-----)(.*?)(-----END [A-Z ]*PRIVATE KEY-----)|(-----BEGIN [A-Z ]*PRIVATE KEY-----)(.*)$`)

// urlCreds matches the password field of a URL userinfo section.
var urlCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:)([^\s@/]{4,})(@)`)

// assigned matches a quoted string assigned to a secret-sounding name. The
// value must pass isCredentialLike (length, not a path/URL, not a plain identifier,
// and sufficiently random).
var assigned = regexp.MustCompile(
	`(?i)([A-Za-z0-9_.\-]*(?:secret|passwd|password|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credential|auth)[A-Za-z0-9_.\-]*\s*[:=]\s*)` +
		`("[^"\n]{20,}"|'[^'\n]{20,}')`)

// Text masks every secret-shaped value in s, returning the result and how many
// values were masked.
func Text(s string) (string, int) {
	n := 0

	out := pemBody.ReplaceAllStringFunc(s, func(m string) string {
		g := pemBody.FindStringSubmatch(m)
		if g == nil {
			return m
		}
		n++
		// Paired begin/end
		if len(g) > 3 && g[2] != "" {
			return g[1] + "\n" + Mask + "\n" + g[3]
		}
		// Unpaired begin to end of input
		return g[4] + "\n" + Mask
	})
	out = provider.ReplaceAllStringFunc(out, func(string) string {
		n++
		return Mask
	})
	out = urlCreds.ReplaceAllStringFunc(out, func(m string) string {
		g := urlCreds.FindStringSubmatch(m)
		if strings.Contains(g[2], Mask) {
			return m
		}
		n++
		return g[1] + Mask + g[3]
	})
	out = assigned.ReplaceAllStringFunc(out, func(m string) string {
		g := assigned.FindStringSubmatch(m)
		value := strings.Trim(g[2], `"'`)
		if !isCredentialLike(value) {
			return m
		}
		n++
		quote := ""
		if strings.HasPrefix(g[2], `"`) {
			quote = `"`
		} else if strings.HasPrefix(g[2], `'`) {
			quote = `'`
		}
		return g[1] + quote + Mask + quote
	})
	return out, n
}

// isCredentialLike reports whether a value looks like a credential. It rejects
// paths, URLs, filenames, plain identifiers, and low-entropy strings. A value
// must be a rune string of >= 20 characters, not contain path separators or file
// extensions, not match identifier or dotted-selector shapes, and either mix
// 3+ of 4 character classes (lowercase, uppercase, digit, symbol) or have
// Shannon entropy >= 3.5 bits per character.
func isCredentialLike(v string) bool {
	runeLen := len([]rune(v))
	if runeLen < 20 {
		return false
	}
	// Reject paths, URLs, filenames
	if strings.ContainsAny(v, " \t/\\") || strings.Contains(v, "://") {
		return false
	}
	if strings.HasSuffix(v, `"`) || strings.HasSuffix(v, `'`) {
		return false
	}
	matched, _ := regexp.MatchString(`\.[A-Za-z]{1,5}$`, v)
	if matched {
		return false
	}
	// Reject plain identifiers and dotted selectors
	if matched, _ := regexp.MatchString(`^[A-Za-z_][A-Za-z0-9_]*$`, v); matched {
		return false
	}
	if matched, _ := regexp.MatchString(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)+$`, v); matched {
		return false
	}
	if strings.ContainsAny(v, "()") {
		return false
	}

	// Check character class mixing or entropy
	hasLower := false
	hasUpper := false
	hasDigit := false
	hasSymbol := false
	for _, r := range v {
		if r >= 'a' && r <= 'z' {
			hasLower = true
		} else if r >= 'A' && r <= 'Z' {
			hasUpper = true
		} else if r >= '0' && r <= '9' {
			hasDigit = true
		} else {
			hasSymbol = true
		}
	}
	classCount := 0
	if hasLower {
		classCount++
	}
	if hasUpper {
		classCount++
	}
	if hasDigit {
		classCount++
	}
	if hasSymbol {
		classCount++
	}
	if classCount >= 3 {
		return true
	}

	// Check entropy
	freq := make(map[rune]float64)
	for _, r := range v {
		freq[r]++
	}
	total := float64(runeLen)
	var h float64
	for _, c := range freq {
		p := c / total
		h -= p * math.Log2(p)
	}
	return h >= 3.5
}
