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
	return clipSignature(string(src[start:end]))
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
