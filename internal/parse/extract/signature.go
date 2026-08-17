package extract

import (
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

// signatureOf returns a declaration's header: the def node's text up to its
// body, collapsed onto one line and length-capped. For a function or method this
// is the name, parameters, and return type; for a type or class it is the
// declaration line. A node with no body (an interface method, an alias, or a
// grammar that detaches the body) yields its whole text. The result lets `find`
// show a symbol's interface without the agent opening the file.
func signatureOf(def sitter.Node, src []byte) string {
	start, end := def.StartByte(), def.EndByte()
	if body := def.ChildByFieldName("body"); !body.IsNull() && body.StartByte() > start {
		end = body.StartByte()
	} else if b, ok := firstBlockByte(def); ok && b > start {
		end = b
	}
	if end > uint(len(src)) {
		end = uint(len(src))
	}
	// A Go struct or interface exposes no `body` field to stop the header at, so
	// its whole body is flattened into the signature. Elide comment nodes first:
	// an inline `// ...` field comment runs to end-of-line and, once
	// clipSignature collapses newlines to spaces, would swallow the next field
	// (e.g. `Provider Inferencer // when set...` eating the following field).
	var comments [][2]uint
	collectCommentRanges(def, start, end, &comments)
	return clipSignature(elideRanges(src, start, end, comments))
}

// collectCommentRanges appends the byte ranges of comment nodes in def's subtree
// that fall within [start,end), in ascending order (pre-order over disjoint
// comment nodes), so signatureOf can remove them from a flattened declaration.
func collectCommentRanges(n sitter.Node, start, end uint, out *[][2]uint) {
	if n.EndByte() <= start || n.StartByte() >= end {
		return
	}
	if strings.Contains(n.Type(), "comment") {
		a, b := n.StartByte(), n.EndByte()
		if a < start {
			a = start
		}
		if b > end {
			b = end
		}
		*out = append(*out, [2]uint{a, b})
		return
	}
	for i := uint32(0); i < n.NamedChildCount(); i++ {
		collectCommentRanges(n.NamedChild(i), start, end, out)
	}
}

// elideRanges returns src[start:end) with the given (ascending, disjoint) byte
// ranges removed.
func elideRanges(src []byte, start, end uint, ranges [][2]uint) string {
	if len(ranges) == 0 {
		return string(src[start:end])
	}
	var b strings.Builder
	cur := start
	for _, r := range ranges {
		if r[0] > cur {
			b.Write(src[cur:r[0]])
		}
		if r[1] > cur {
			cur = r[1]
		}
	}
	if cur < end {
		b.Write(src[cur:end])
	}
	return b.String()
}

// firstBlockByte returns the start byte of a function or type body that the
// grammar does not expose as a `body` field (for example Kotlin and JavaScript),
// and whether one was found.
func firstBlockByte(def sitter.Node) (uint, bool) {
	for i := uint32(0); i < def.NamedChildCount(); i++ {
		switch c := def.NamedChild(i); c.Type() {
		case "block", "statement_block", "compound_statement", "function_body",
			"block_statement", "class_body", "declaration_list", "enum_body",
			"field_declaration_list", "enum_class_body", "enum_declaration_list",
			"object_body", "extension_body":
			return c.StartByte(), true
		}
	}
	return 0, false
}

// clipSignature collapses whitespace runs to single spaces, trims, and caps the
// length so a signature stays a token-lean one-liner.
func clipSignature(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 200
	if len(s) > max {
		s = strings.TrimSpace(s[:max]) + " ..."
	}
	return s
}
