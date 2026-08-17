// Package redact masks secret-shaped values before they are stored. prowl indexes
// whatever a repository contains, including credentials committed in source, and
// every retrieval path emits stored text into an agent's context. Masking at index
// time rather than on output is deliberate: an output filter still leaves the
// on-disk index a cleartext secret store.
//
// The identifier and structure around a value are preserved, so "where is the
// stripe token set" still resolves; only the value itself is destroyed.
//
// Masking is destructive and happens before storage: there is no cleartext copy
// anywhere, so a false positive permanently corrupts indexed source. Precision
// therefore outranks recall. Text masks exactly three things, each with a shape
// unambiguous enough to have produced zero false positives across review:
// provider credentials (vendor-prefixed keys, AWS key ids, Google keys, JWTs),
// the password field of a URL userinfo section, and PEM private key bodies.
//
// A homegrown credential with no vendor prefix -- a random-looking value assigned
// to a secret-named variable -- is deliberately NOT masked here. A generic entropy
// or character-class heuristic cannot separate it from ordinary code without
// eventually masking real source, and because masking is destructive that is an
// unacceptable trade. An unreliable signal belongs in a non-destructive warning,
// never in destructive masking, so the recall loss is accepted on purpose.
package redact

import (
	"regexp"
	"strings"
)

// Mask replaces a detected secret value.
const Mask = "[redacted]"

// provider matches credentials whose shape is unambiguous: a vendor prefix, an
// AWS key id, a Google key, or a JWT. These need no entropy check.
var provider = regexp.MustCompile(
	`(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{16,}` +
		`|\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}` +
		`|gh[pousr]_[A-Za-z0-9]{20,}` +
		`|xox[abposr]-[A-Za-z0-9-]{10,}` +
		`|AKIA[0-9A-Z]{16}` +
		`|AIza[0-9A-Za-z_-]{35}` +
		`|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)

// urlCreds matches the password field of a URL userinfo section.
var urlCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:)([^\s@/]{4,})(@)`)

// PEM private keys are matched by three separate patterns applied in sequence
// (see Text). A single alternation was the root cause of three defects: Go's
// leftmost-match rule could prefer a start-anchored tail alternative over the
// paired one and destroy all preceding source. Keeping the patterns distinct lets
// each retain its markers and increment the count only when it rewrites text.

// pemPaired matches a complete private key block: both markers with a body
// between them. Only the body is replaced; both markers are kept.
var pemPaired = regexp.MustCompile(`(?s)(-----BEGIN [A-Z ]*PRIVATE KEY-----)(.*?)(-----END [A-Z ]*PRIVATE KEY-----)`)

// pemBegin matches the head half of a key split across chunks: a BEGIN marker
// with a body and no END after it. Everything after the marker is masked.
var pemBegin = regexp.MustCompile(`(?s)(-----BEGIN [A-Z ]*PRIVATE KEY-----)(.+)$`)

// pemEnd matches the tail half of a split key: a body and an END marker with no
// BEGIN before it. Everything up to the marker is masked.
var pemEnd = regexp.MustCompile(`(?s)^(.+?)(-----END [A-Z ]*PRIVATE KEY-----)`)

// Text masks every secret-shaped value in s, returning the result and how many
// values were masked. The count is idempotent: running Text twice on the same
// input masks nothing the second time.
func Text(s string) (string, int) {
	n := 0

	// PEM keys: the paired pattern owns any input that contains a complete block.
	// The unpaired head/tail patterns run only when no complete block was found,
	// so they cannot interact with a paired match.
	if pemPaired.MatchString(s) {
		s = pemPaired.ReplaceAllStringFunc(s, func(m string) string {
			g := pemPaired.FindStringSubmatch(m)
			// An empty or already-masked body has nothing to rewrite.
			if g[2] == "" || strings.Contains(g[2], Mask) {
				return m
			}
			n++
			return g[1] + "\n" + Mask + "\n" + g[3]
		})
	} else {
		s = pemBegin.ReplaceAllStringFunc(s, func(m string) string {
			g := pemBegin.FindStringSubmatch(m)
			if strings.Contains(g[2], Mask) {
				return m
			}
			n++
			return g[1] + "\n" + Mask
		})
		s = pemEnd.ReplaceAllStringFunc(s, func(m string) string {
			g := pemEnd.FindStringSubmatch(m)
			if strings.Contains(g[1], Mask) {
				return m
			}
			n++
			return Mask + "\n" + g[2]
		})
	}

	s = provider.ReplaceAllStringFunc(s, func(string) string {
		n++
		return Mask
	})
	s = urlCreds.ReplaceAllStringFunc(s, func(m string) string {
		g := urlCreds.FindStringSubmatch(m)
		if strings.Contains(g[2], Mask) {
			return m
		}
		n++
		return g[1] + Mask + g[3]
	})
	return s, n
}
