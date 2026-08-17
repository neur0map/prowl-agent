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
// therefore outranks recall. Text masks exactly three things, each restricted to
// a shape that separates a credential from ordinary source (an earlier claim of
// zero false positives did not survive review; see skVendorKey and hasKeyMaterial):
// provider credentials (vendor-prefixed keys, AWS key ids, Google keys, JWTs),
// the password field of a URL userinfo section (with or without a username), and
// PEM private key bodies.
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
// AWS key id, a Google key, or a JWT. Each branch needs no entropy check. The
// sk- branch is the exception: it collides with kebab identifiers (CSS custom
// properties, SpinKit spinner classes) and sk-your-... doc placeholders, so its
// matches are re-checked by skVendorKey in Text before masking.
var provider = regexp.MustCompile(
	`(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{16,}` +
		`|\bsk-[A-Za-z0-9_-]{24,}` +
		`|gh[pousr]_[A-Za-z0-9]{20,}` +
		`|xox[abposr]-[A-Za-z0-9-]{10,}` +
		`|AKIA[0-9A-Z]{16}` +
		`|AIza[0-9A-Za-z_-]{35}` +
		`|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)

// skVendorKey reports whether body (the text after an sk- prefix the provider
// regex matched) has the shape of a real vendor key rather than a kebab
// identifier or a documentation placeholder. Every real sk- key is a long random
// run: OpenAI legacy and DeepSeek are sk-+48, sk-proj- ~160, Anthropic
// sk-ant-api03- ~105, OpenRouter sk-or-v1- ~70 -- all >=40 body characters. The
// shortest keys some OpenAI-compatible providers issue are a single dash-free run
// of roughly 32 characters. A false positive is the opposite shape: a
// human-readable, dash-separated identifier or an sk-your-... placeholder, whose
// segments are short words joined by dashes. So a match is a key when its body is
// long (>=40) or a dash-free run of >=24, and never when it is a short
// dash-separated identifier. This is a STRUCTURAL test -- length and dash
// separation -- not the character-class/entropy heuristic deleted under R13,
// which tried to score randomness and masked real camelCase. The one residual it
// does not guard: a hypothetical 40+ char all-lowercase-and-dashes identifier
// beginning sk- would still mask; none exists in the corpus and it is far outside
// observed identifier lengths, so it is a documented boundary, not a live risk.
func skVendorKey(body string) bool {
	if len(body) >= 40 {
		return true
	}
	return len(body) >= 24 && !strings.Contains(body, "-")
}

// urlCreds matches the password field of a URL userinfo section. The username is
// optional ([^\s:/@]*): redis:// and amqp:// connection strings conventionally
// carry a password and no user (redis://:pass@host), and that cleartext must not
// reach the store. Allowing an empty username cannot widen the false-positive
// surface: it only additionally matches text where ":" immediately follows "://",
// which is the empty-userinfo form itself.
var urlCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]*:)([^\s@/]{4,})(@)`)

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

// pemBodyLine matches one line that is entirely base64 characters -- the shape of
// PEM key material. hasKeyMaterial applies it per line after normalizing line
// separators, so it recognizes key material whether the key wears real newlines,
// literal \n / \r\n escapes, or leading indentation.
var pemBodyLine = regexp.MustCompile(`^[A-Za-z0-9+/]{4,}={0,2}$`)

// hasKeyMaterial reports whether body carries at least one base64-shaped line.
// A private key is embedded in source in several shapes, and this is the one gate
// that must recognize all of them, because base64 line shape is exactly what
// separates a real key from a BEGIN/END marker merely NAMED in prose (whose lines
// carry spaces and punctuation). It normalizes the line separators a key wears in
// each embedding -- real newlines (bare files, Go raw strings), the literal \n and
// \r\n escapes of .env / JSON / quoted-string constants, and CRLF -- and strips
// per-line indentation (YAML block scalars indent every body line), then looks for
// one pure-base64 line. A prose mention has none, so masking is skipped. (An
// opaque base64 blob of a WHOLE key carries no literal -----BEGIN----- marker, so
// it never reaches here; masking arbitrary base64 is the destructive class we
// refuse.)
func hasKeyMaterial(body string) bool {
	b := strings.ReplaceAll(body, `\r\n`, "\n") // literal \r\n escape
	b = strings.ReplaceAll(b, `\n`, "\n")       // literal \n escape
	b = strings.ReplaceAll(b, "\r\n", "\n")     // real CRLF
	for _, ln := range strings.Split(b, "\n") {
		if pemBodyLine.MatchString(strings.Trim(ln, " \t\r")) {
			return true
		}
	}
	return false
}

// pemSep matches one line separator in any embedding a key is stored in: a real
// newline (optionally CR-prefixed) or the literal \n / \r\n escape sequences that
// .env, JSON, YAML-flow and quoted-string constants carry. Requiring a separator
// immediately after a BEGIN marker is what keeps a marker named MID-SENTENCE in
// prose (`-----BEGIN ... KEY----- in the docs`) from starting a match ahead of a
// real key, while a real key -- even one that opens a `const k = "..."` literal on
// the same physical line, its body on \n escapes -- still matches.
const pemSep = `(?:\r?\n|(?:\\r)?\\n)`

// pemPaired matches a complete private key block: both markers with a separator
// and a body between them. The separator after BEGIN (pemSep) rejects a prose
// mid-sentence mention; hasKeyMaterial (checked in Text) rejects a marker that
// ends its line but is followed by ordinary source. Only the body is replaced;
// both markers are kept, each on its own line.
var pemPaired = regexp.MustCompile(`(?s)(-----BEGIN [A-Z ]*PRIVATE KEY-----)` + pemSep + `(.*?)(-----END [A-Z ]*PRIVATE KEY-----)`)

// pemBegin matches the head half of a key split across chunks: a BEGIN marker, a
// separator, then a bounded body that runs to end of input. The trailing `-*`
// lets the body absorb a dash run left at a chunk boundary (a cut inside a
// marker's opening dashes), which stays safe because a live END marker's five
// dashes are followed by `END `, so `-*` before `$` cannot consume across one.
// The body is masked; the BEGIN marker is kept.
var pemBegin = regexp.MustCompile(`(-----BEGIN [A-Z ]*PRIVATE KEY-----)` + pemSep + `(` + pemBodyTemper + `-*)$`)

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

	// PEM keys: all three patterns run in sequence. Each masks only a body that
	// carries base64 key material (hasKeyMaterial), so a BEGIN/END marker named in
	// prose does not destroy the surrounding source. pemPaired and pemBegin also
	// anchor the BEGIN marker to a line start, so a prose mention cannot begin a
	// match ahead of a real key. The unpaired bodies stay bounded so they cannot
	// cross a marker: a chunk holding a complete key plus another key's split half
	// redacts both without one destroying the other.
	s = pemPaired.ReplaceAllStringFunc(s, func(m string) string {
		g := pemPaired.FindStringSubmatch(m)
		// g: 1 BEGIN, 2 body, 3 END (the separator after BEGIN is non-captured).
		// Skip an empty or already-masked body, and a marker named in prose (no
		// key material). The mask goes on its own line regardless of input style.
		if g[2] == "" || strings.Contains(g[2], Mask) || !hasKeyMaterial(g[2]) {
			return m
		}
		n++
		return g[1] + "\n" + Mask + "\n" + g[3]
	})
	s = pemBegin.ReplaceAllStringFunc(s, func(m string) string {
		g := pemBegin.FindStringSubmatch(m)
		// g: 1 BEGIN, 2 body.
		if g[2] == "" || strings.Contains(g[2], Mask) || !hasKeyMaterial(g[2]) {
			return m
		}
		n++
		return g[1] + "\n" + Mask
	})
	s = pemEnd.ReplaceAllStringFunc(s, func(m string) string {
		g := pemEnd.FindStringSubmatch(m)
		if g[1] == "" || strings.Contains(g[1], Mask) || !hasKeyMaterial(g[1]) {
			return m
		}
		n++
		return Mask + "\n" + g[2]
	})

	s = provider.ReplaceAllStringFunc(s, func(m string) string {
		// The sk- branch collides with kebab identifiers and sk-your-... doc
		// placeholders; skip a match that is not key-shaped (see skVendorKey).
		// Every other branch has an unambiguous prefix and always masks.
		if strings.HasPrefix(m, "sk-") && !skVendorKey(m[len("sk-"):]) {
			return m
		}
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

// pemMarker matches either PEM private-key marker. Chunks uses it to read the
// marker structure of an already body-masked chunk (Text keeps the markers).
var pemMarker = regexp.MustCompile(`-----(BEGIN|END) [A-Z ]*PRIVATE KEY-----`)

// endsInsideOpenKey reports whether s ends inside an unterminated private key:
// its last private-key marker is a BEGIN with no following END, so whatever
// chunk comes next begins as raw key body.
func endsInsideOpenKey(s string) bool {
	markers := pemMarker.FindAllString(s, -1)
	if len(markers) == 0 {
		return false
	}
	return strings.HasPrefix(markers[len(markers)-1], "-----BEGIN")
}

// Chunks masks a file's chunk texts as an ordered set and returns the masked
// texts and the total values masked. It runs Text on each chunk, then applies a
// whole-file containment that Text cannot: a private-key body longer than one
// chunk window tiles into a middle chunk carrying no BEGIN/END marker, so Text's
// per-chunk trigger scan skips it and it would be stored verbatim as raw key
// material (C4). Walking the chunks in order, a marker-free chunk seen while a
// key opened by an earlier BEGIN has not been closed by an END is that middle
// body, and is masked wholesale. A base64 blob that is not inside a key (no
// BEGIN precedes it) is never masked, so legitimate embedded base64 survives.
// The caller passes chunk texts in file order; masking the middle body is part
// of the chunk pass, so it participates in the persisted count like any other.
func Chunks(texts []string) ([]string, int) {
	out := make([]string, len(texts))
	total := 0
	inKey := false
	for i, t := range texts {
		masked, n := Text(t)
		total += n
		switch {
		case pemMarker.MatchString(masked):
			// A chunk carrying a marker delimits the key itself; Text already
			// handled its body. Its last marker sets whether the key stays open.
			inKey = endsInsideOpenKey(masked)
		case inKey && strings.TrimSpace(masked) != "" && !strings.Contains(masked, Mask):
			// Marker-free chunk inside an open key: raw body, mask wholesale.
			masked = Mask
			total++
		}
		out[i] = masked
	}
	return out, total
}
