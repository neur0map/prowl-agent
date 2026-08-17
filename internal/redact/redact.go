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

// placeholderPassword reports whether the userinfo password p is a documentation
// placeholder rather than a real credential, so URL examples in prose --
// "scheme://user:pass@host" -- survive byte-identical. urlCreds accepts a
// four-character password, too short to separate a common placeholder from a
// weak credential, so the ambiguous set is enumerated here. Raising the length
// floor instead would miss real weak passwords, so the skip is by value.
func placeholderPassword(p string) bool {
	switch strings.ToLower(p) {
	case "pass", "password", "passwd", "secret", "token", "user", "username",
		"changeme", "example", "test", "xxx", "redacted", "yourpassword", "hunter2":
		return true
	}
	if p != "" && strings.Trim(p, "*") == "" {
		return true // wholly asterisks
	}
	if strings.HasPrefix(p, "<") && strings.HasSuffix(p, ">") {
		return true // <angle-bracket placeholder>
	}
	if strings.HasPrefix(p, "${") && strings.HasSuffix(p, "}") {
		return true // ${ENV_VAR} interpolation
	}
	return false
}

// PEM private keys are matched by three separate patterns applied in sequence
// and unconditionally (see Text). A single alternation was the root cause of
// three defects: Go's leftmost-match rule could prefer a start-anchored tail
// alternative over the paired one and destroy all preceding source. The unpaired
// patterns are bounded so their bodies cannot cross a marker boundary, which is
// what lets all three run unconditionally without one key's split half leaking
// past another key that shares the chunk.
//
// The bound is expressed by pemBodyTemper below. Go's regexp is RE2, which has
// no lookahead, so the body cannot be written as a tempered `-(?!----END)` token.
// Instead the body admits runs of at most four dashes. The premise that makes
// this sound: the only five-dash runs in a PEM stream are the BEGIN/END markers
// themselves -- base64 payloads and headers such as `DEK-Info` never contain five
// consecutive dashes. Both unpaired consumers are anchored (`$` for pemBegin, `^`
// for pemEnd), so a five-dash run does NOT truncate the body; it makes the whole
// match FAIL, because no bounded body can consume across the run to reach its
// anchor. For a live marker of the opposite kind that failure is the intended
// outcome -- the body stops before the marker and the marker survives. For a
// marker truncated by a chunk cut it instead costs recall (the split half is left
// unmasked), which is unreachable today because prowl chunks on line boundaries
// (internal/parse/extract/extract.go splits on "\n"), so a five-dash marker always
// sits wholly within one line and no chunk cut can truncate it.
const pemBodyTemper = `(?:[^-]|-{1,4}[^-])*-{0,4}`

// pemPaired matches a complete private key block: both markers with a body
// between them. Only the body is replaced; both markers are kept.
var pemPaired = regexp.MustCompile(`(?s)(-----BEGIN [A-Z ]*PRIVATE KEY-----)(.*?)(-----END [A-Z ]*PRIVATE KEY-----)`)

// pemBegin matches the head half of a key split across chunks: a BEGIN marker
// whose bounded body runs to end of input. The trailing `-*` lets the body absorb
// a dash run left at a chunk boundary (a cut inside a marker's opening dashes),
// which stays safe because a live END marker's five dashes are followed by `END `,
// so `-*` before `$` cannot consume across one. The body is masked; marker kept.
var pemBegin = regexp.MustCompile(`(-----BEGIN [A-Z ]*PRIVATE KEY-----)(` + pemBodyTemper + `-*)$`)

// pemEnd matches the tail half of a split key: a body from start of input that
// reaches an END marker without crossing a BEGIN marker. The body is masked; the
// marker is kept.
var pemEnd = regexp.MustCompile(`^(` + pemBodyTemper + `)(-----END [A-Z ]*PRIVATE KEY-----)`)

// triggers are the literal substrings some pattern actually requires: the URL
// "://", the PEM "-----", and each provider prefix (the GitHub-token prefixes
// are spelled out because the pattern requires gh + one of p/o/u/s/r + "_", not a
// bare "gh"). A string containing none of them cannot match any pattern, so Text
// returns it untouched without running a regex. Quote characters are deliberately
// not triggers: no pattern requires them, and because almost all source is quoted
// they would defeat the fast path. The list is derived from the patterns' own
// literals so it cannot drift; a new pattern with a new literal must add it here.
var triggers = []string{"://", "-----", "sk_", "sk-", "pk_", "rk_", "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "xox", "AKIA", "AIza", "eyJ"}

func hasTrigger(s string) bool {
	for _, t := range triggers {
		if strings.Contains(s, t) {
			return true
		}
	}
	return false
}

// Text masks every secret-shaped value in s, returning the result and how many
// values were masked. The count is idempotent: running Text twice on the same
// input masks nothing the second time.
func Text(s string) (string, int) {
	// Fast path: a string carrying no pattern literal cannot match, so skip the
	// regex passes entirely. Most chunks, signatures, and edge targets have no
	// credential shape at all, and this is lossless by construction (see triggers).
	if !hasTrigger(s) {
		return s, 0
	}
	n := 0

	// PEM keys: all three patterns run unconditionally. The unpaired bodies are
	// bounded so they cannot cross a marker, so a chunk holding a complete key
	// plus another key's split half redacts both without one destroying the other.
	s = pemPaired.ReplaceAllStringFunc(s, func(m string) string {
		g := pemPaired.FindStringSubmatch(m)
		// An empty or already-masked body has nothing to rewrite.
		if g[2] == "" || strings.Contains(g[2], Mask) {
			return m
		}
		n++
		return g[1] + "\n" + Mask + "\n" + g[3]
	})
	s = pemBegin.ReplaceAllStringFunc(s, func(m string) string {
		g := pemBegin.FindStringSubmatch(m)
		if g[2] == "" || strings.Contains(g[2], Mask) {
			return m
		}
		n++
		return g[1] + "\n" + Mask
	})
	s = pemEnd.ReplaceAllStringFunc(s, func(m string) string {
		g := pemEnd.FindStringSubmatch(m)
		if g[1] == "" || strings.Contains(g[1], Mask) {
			return m
		}
		n++
		return Mask + "\n" + g[2]
	})

	s = provider.ReplaceAllStringFunc(s, func(string) string {
		n++
		return Mask
	})
	s = urlCreds.ReplaceAllStringFunc(s, func(m string) string {
		g := urlCreds.FindStringSubmatch(m)
		if placeholderPassword(g[2]) || strings.Contains(g[2], Mask) {
			return m
		}
		n++
		return g[1] + Mask + g[3]
	})
	return s, n
}
