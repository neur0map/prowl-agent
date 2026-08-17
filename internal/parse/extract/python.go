package extract

import sitter "github.com/alexaandru/go-tree-sitter-bare"

func init() { register(pythonExtractor{}) }

type pythonExtractor struct{}

func (pythonExtractor) Lang() string { return "python" }

const pythonSCM = `
(function_definition name: (identifier) @func.name) @func.def
(class_definition name: (identifier) @class.name) @class.def
(import_statement name: (dotted_name) @import.mod)
(import_from_statement module_name: (dotted_name) @import.mod)
(import_from_statement module_name: (relative_import) @import.rel)
`

func (pythonExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("python", src, []byte(pythonSCM), func(caps []capture) {
		if n, ok := capNode(caps, "func.name"); ok {
			end, cx, sig, doc := line(n), 1, "", ""
			if d, ok := capNode(caps, "func.def"); ok {
				end, cx, sig, doc = endLine(d), complexity(d, "python"), signatureOf(d, src), pythonDocstring(d, src)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "function", Signature: sig, StartLine: line(n), EndLine: end, Complexity: cx, Doc: doc})
		}
		if n, ok := capNode(caps, "class.name"); ok {
			end, sig, doc := line(n), "", ""
			if d, ok := capNode(caps, "class.def"); ok {
				end, sig, doc = endLine(d), signatureOf(d, src), pythonDocstring(d, src)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "class", Signature: sig, StartLine: line(n), EndLine: end, Doc: doc})
		}
		if n, ok := capNode(caps, "import.mod"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
		if n, ok := capNode(caps, "import.rel"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
	})
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, err
}

// pythonDocstring returns the docstring that opens a function or class body: the
// first statement of the body when it is a string literal, stripped to its inner
// text. Python's contract lives in this docstring, which sits inside the body
// rather than in a leading comment, so the generic upward comment walk cannot
// see it; capturing it here lets that high-signal prose be scored on its own.
func pythonDocstring(def sitter.Node, src []byte) string {
	body, ok := firstChildOfType(def, "block")
	if !ok || body.NamedChildCount() == 0 {
		return ""
	}
	first := body.NamedChild(0)
	if first.Type() != "expression_statement" {
		return ""
	}
	str, ok := firstChildOfType(first, "string")
	if !ok {
		return ""
	}
	if content, ok := firstChildOfType(str, "string_content"); ok {
		return content.Content(src)
	}
	return str.Content(src)
}
