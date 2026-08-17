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
// (begin/end) or unpaired (begin to end of input, or start to end).
var pemBody = regexp.MustCompile(`(?s)(-----BEGIN [A-Z ]*PRIVATE KEY-----)(.*?)(-----END [A-Z ]*PRIVATE KEY-----)|(-----BEGIN [A-Z ]*PRIVATE KEY-----)(.*)$|(^.*?)(-----END [A-Z ]*PRIVATE KEY-----)`)

// urlCreds matches the password field of a URL userinfo section.
var urlCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:)([^\s@/]{4,})(@)`)

// assigned matches a quoted string assigned to a secret-sounding name. The
// value must pass isCredentialLike (length, not a path/URL, not a plain identifier,
// and sufficiently random).
var assigned = regexp.MustCompile(
	`(?i)([A-Za-z0-9_.\-]*(?:secret|passwd|password|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credential|auth)[A-Za-z0-9_.\-]*\s*[:=]\s*)` +
		`("[^"\n]{20,}"|'[^'\n]{20,}')`)

// identifierShapeRe matches values that look like plain code identifiers.
var identifierShapeRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// filenameOrTLDRe matches values ending in a file extension or TLD.
var filenameOrTLDRe = regexp.MustCompile(`\.[A-Za-z]{1,5}$`)

// dottedSelectorRe matches dotted-selector shapes like a.b.c.
var dottedSelectorRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)+$`)

// Text masks every secret-shaped value in s, returning the result and how many
// values were masked.
func Text(s string) (string, int) {
	n := 0

	out := pemBody.ReplaceAllStringFunc(s, func(m string) string {
		// Guard against recounting already-masked content
		if strings.Contains(m, Mask) {
			return m
		}
		g := pemBody.FindStringSubmatch(m)
		if g == nil {
			return m
		}
		n++
		// Paired begin/end
		if len(g) > 2 && g[2] != "" {
			return g[1] + "\n" + Mask + "\n" + g[3]
		}
		// Unpaired begin to end of input
		if len(g) > 4 && g[4] != "" {
			return g[4] + "\n" + Mask
		}
		// Unpaired end (tail of split key)
		if len(g) > 6 && g[6] != "" {
			return Mask
		}
		return m
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

// isCredentialLike reports whether a value looks like a credential. It masks when
// ANY of these hold: 3+ character classes; 20%+ digit density; or 3.5+ entropy.
// It rejects filenames/TLDs, dotted selectors, parentheses, and values that match
// identifier shapes with <2 digits and high vowel ratio.
func isCredentialLike(v string) bool {
	runeLen := len([]rune(v))
	if runeLen < 20 {
		return false
	}

	// Reject filenames and TLDs, or URLs
	if filenameOrTLDRe.MatchString(v) || strings.Contains(v, "://") {
		return false
	}

	// Reject dotted selectors and parentheses
	if dottedSelectorRe.MatchString(v) || strings.ContainsAny(v, "()") {
		return false
	}

	// Reject identifier-shaped values that are plain code identifiers.
	// A code identifier is identifier-shaped AND has <2 digits AND high vowel ratio.
	if isPlainCodeIdentifier(v) {
		return false
	}

	// Count character classes
	hasLower := false
	hasUpper := false
	hasDigit := false
	hasSymbol := false
	digitCount := 0

	for _, r := range v {
		if r >= 'a' && r <= 'z' {
			hasLower = true
		} else if r >= 'A' && r <= 'Z' {
			hasUpper = true
		} else if r >= '0' && r <= '9' {
			hasDigit = true
			digitCount++
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

	// Accept if 3+ classes
	if classCount >= 3 {
		return true
	}

	// Accept if digit density >= 20%
	digitDensity := float64(digitCount) / float64(runeLen)
	if digitDensity >= 0.20 {
		return true
	}

	// Accept if entropy >= 3.5
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
	if h >= 3.5 {
		return true
	}

	return false
}

// isPlainCodeIdentifier reports whether a value is a code identifier that should
// not be masked. It must be identifier-shaped, contain <2 digits, and have a high
// vowel ratio (>= 0.25 of its letters), typical of English-like variable names.
func isPlainCodeIdentifier(v string) bool {
	if !identifierShapeRe.MatchString(v) {
		return false
	}

	digitCount := 0
	for _, r := range v {
		if r >= '0' && r <= '9' {
			digitCount++
		}
	}
	if digitCount >= 2 {
		return false
	}

	// Count vowels among letters
	vowelCount := 0
	letterCount := 0
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			letterCount++
			switch r {
			case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
				vowelCount++
			}
		}
	}

	if letterCount == 0 {
		return false
	}

	vowelRatio := float64(vowelCount) / float64(letterCount)
	return vowelRatio >= 0.25
}
