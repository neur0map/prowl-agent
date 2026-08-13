package embed

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// tokenizeIDs turns text into vocabulary IDs using the same recipe HuggingFace's
// WordPiece tokenizer applies for the bert-base-uncased / bge family the potion
// model was distilled from: BertNormalizer (clean, CJK spacing, strip accents,
// lowercase) -> BertPreTokenizer (split on whitespace and punctuation) ->
// greedy WordPiece with a "##" continuation prefix. No special tokens are added,
// matching model2vec's add_special_tokens=False. Any word with no matching
// subword becomes the unknown token.
func (m *Model) tokenizeIDs(text string) []int {
	var ids []int
	for _, word := range preTokenize(normalizeText(text)) {
		m.wordpiece(word, &ids)
	}
	return ids
}

// normalizeText applies BertNormalizer: control/whitespace cleanup, spaces
// around CJK characters, accent stripping (NFD, drop combining marks), and
// lowercasing.
func normalizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case r == 0 || r == 0xFFFD || isControl(r):
			// dropped
		case isWhitespace(r):
			b.WriteByte(' ')
		case isCJK(r):
			b.WriteByte(' ')
			b.WriteRune(r)
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	// strip_accents (defaults to lowercase=true here) then lowercase.
	var out strings.Builder
	out.Grow(b.Len())
	for _, r := range norm.NFD.String(b.String()) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		out.WriteRune(unicode.ToLower(r))
	}
	return out.String()
}

// preTokenize splits normalized text on whitespace and emits each punctuation
// character as its own token (BertPreTokenizer).
func preTokenize(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == ' ' || isWhitespace(r):
			flush()
		case isPunct(r):
			flush()
			out = append(out, string(r))
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// wordpiece appends the greedy longest-match subword IDs for one word, or the
// unknown-token ID if the word is too long or cannot be fully segmented.
func (m *Model) wordpiece(word string, ids *[]int) {
	runes := []rune(word)
	if len(runes) == 0 {
		return
	}
	if len(runes) > m.maxChars {
		*ids = append(*ids, m.unkID)
		return
	}
	pieces := make([]int, 0, 4)
	for start := 0; start < len(runes); {
		end := len(runes)
		id := -1
		for end > start {
			sub := string(runes[start:end])
			if start > 0 {
				sub = "##" + sub
			}
			if v, ok := m.vocab[sub]; ok {
				id = v
				break
			}
			end--
		}
		if id < 0 {
			// A word is unknown as a whole if any segment fails to match.
			*ids = append(*ids, m.unkID)
			return
		}
		pieces = append(pieces, id)
		start = end
	}
	*ids = append(*ids, pieces...)
}

// isControl reports whether r is a Unicode control character (category C*),
// excluding the tab/newline/carriage-return already handled as whitespace.
func isControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return unicode.In(r, unicode.C)
}

// isWhitespace reports whether r is a space separator (BERT treats these, plus
// tab/newline/CR, as whitespace).
func isWhitespace(r rune) bool {
	return r == ' ' || unicode.Is(unicode.Zs, r)
}

// isPunct matches BERT's punctuation test: the ASCII punctuation ranges plus any
// Unicode punctuation category.
func isPunct(r rune) bool {
	if (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) || (r >= 123 && r <= 126) {
		return true
	}
	return unicode.IsPunct(r)
}

// isCJK reports whether r is in a CJK block that BERT space-pads for per-
// character tokenization.
func isCJK(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x20000 && r <= 0x2A6DF,
		r >= 0x2A700 && r <= 0x2B73F,
		r >= 0x2B740 && r <= 0x2B81F,
		r >= 0x2B820 && r <= 0x2CEAF,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0x2F800 && r <= 0x2FA1F:
		return true
	}
	return false
}
