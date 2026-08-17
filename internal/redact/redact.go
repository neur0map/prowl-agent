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
// AWS key id, a JWT, or a PEM body. These need no entropy check.
var provider = regexp.MustCompile(
	`(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{16,}` +
		`|sk-(?:proj-)?[A-Za-z0-9_-]{20,}` +
		`|gh[pousr]_[A-Za-z0-9]{20,}` +
		`|xox[abposr]-[A-Za-z0-9-]{10,}` +
		`|AKIA[0-9A-Z]{16}` +
		`|AIza[0-9A-Za-z_-]{35}` +
		`|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)

// pemBody matches the base64 payload inside a private key block.
var pemBody = regexp.MustCompile(`(?s)(-----BEGIN [A-Z ]*PRIVATE KEY-----)(.*?)(-----END [A-Z ]*PRIVATE KEY-----)`)

// urlCreds matches the password field of a URL userinfo section.
var urlCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:)([^\s@/]{4,})(@)`)

// assigned matches a quoted or bare value assigned to a secret-sounding name. The
// name alone is not enough, so the value must also look random (see randomEnough).
var assigned = regexp.MustCompile(
	`(?i)([A-Za-z0-9_.\-]*(?:secret|passwd|password|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credential|auth)[A-Za-z0-9_.\-]*\s*[:=]\s*)` +
		`("[^"\n]{12,}"|'[^'\n]{12,}'|[A-Za-z0-9_\-./+=]{12,})`)

// Text masks every secret-shaped value in s, returning the result and how many
// values were masked.
func Text(s string) (string, int) {
	n := 0

	out := pemBody.ReplaceAllStringFunc(s, func(m string) string {
		g := pemBody.FindStringSubmatch(m)
		n++
		return g[1] + "\n" + Mask + "\n" + g[3]
	})
	out = provider.ReplaceAllStringFunc(out, func(string) string {
		n++
		return Mask
	})
	out = urlCreds.ReplaceAllStringFunc(out, func(m string) string {
		g := urlCreds.FindStringSubmatch(m)
		n++
		return g[1] + Mask + g[3]
	})
	out = assigned.ReplaceAllStringFunc(out, func(m string) string {
		g := assigned.FindStringSubmatch(m)
		value := strings.Trim(g[2], `"'`)
		if !randomEnough(value) {
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

// randomEnough reports whether a value carries enough Shannon entropy per
// character to be a credential rather than prose, a path, or an identifier. The
// threshold keeps `password = "changeme"` and `token = "TODO"` out, which is
// correct: those are not secrets, and masking them would hide real code.
func randomEnough(v string) bool {
	if len(v) < 12 {
		return false
	}
	if strings.ContainsAny(v, " \t") || strings.Contains(v, "://") {
		return false
	}
	freq := map[rune]float64{}
	for _, r := range v {
		freq[r]++
	}
	total := float64(len([]rune(v)))
	var h float64
	for _, c := range freq {
		p := c / total
		h -= p * math.Log2(p)
	}
	return h >= 3.2
}
