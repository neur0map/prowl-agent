package extract

func init() { register(javascriptExtractor{}) }

type javascriptExtractor struct{}

func (javascriptExtractor) Lang() string { return "javascript" }

// Named declarations become symbols; module-level variables are captured (so a
// config object like `var data = {...}` is found) but locals inside function
// bodies are not. import/require targets become includes edges.
const javascriptSCM = `
(function_declaration name: (identifier) @func.name) @func.def
(generator_function_declaration name: (identifier) @func.name) @func.def
(class_declaration name: (identifier) @class.name) @class.def
(method_definition name: (property_identifier) @method.name) @method.def
(import_statement source: (string (string_fragment) @import.src))
(call_expression function: (identifier) @req.fn arguments: (arguments (string (string_fragment) @req.src)))
(program (lexical_declaration (variable_declarator name: (identifier) @var.name value: (_) @var.value)))
(program (variable_declaration (variable_declarator name: (identifier) @var.name value: (_) @var.value)))
(program (export_statement declaration: (lexical_declaration (variable_declarator name: (identifier) @var.name value: (_) @var.value))))
(program (export_statement declaration: (variable_declaration (variable_declarator name: (identifier) @var.name value: (_) @var.value))))
`

func (javascriptExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("javascript", src, []byte(javascriptSCM), func(caps []capture) {
		addNamed(&r, caps, src, "func.name", "func.def", "function", "javascript")
		addNamed(&r, caps, src, "class.name", "class.def", "class", "javascript")
		addNamed(&r, caps, src, "method.name", "method.def", "method", "javascript")
		if n, ok := capNode(caps, "import.src"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
		if fn, ok := capNode(caps, "req.fn"); ok && fn.Content(src) == "require" {
			if s, ok := capNode(caps, "req.src"); ok {
				r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: s.Content(src), Line: line(s)})
			}
		}
		if n, ok := capNode(caps, "var.name"); ok {
			kind, sig := "variable", ""
			end, cx := line(n), 0
			if v, ok := capNode(caps, "var.value"); ok {
				end = endLine(v)
				if jsIsFunc(v.Type()) {
					kind, cx, sig = "function", complexity(v, "javascript"), signatureOf(v, src)
				}
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: kind, Signature: sig, StartLine: line(n), EndLine: end, Complexity: cx})
		}
	})
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, err
}

// addNamed appends a symbol from a name capture, using the def capture for the
// end line, declaration signature, and (for function/method kinds) cyclomatic
// complexity from the def node (lang selects the decision-point set).
func addNamed(r *Result, caps []capture, src []byte, nameCap, defCap, kind, lang string) {
	n, ok := capNode(caps, nameCap)
	if !ok {
		return
	}
	end, cx, sig := line(n), 0, ""
	if d, ok := capNode(caps, defCap); ok {
		end = endLine(d)
		sig = signatureOf(d, src)
		if kind == "function" || kind == "method" {
			cx = complexity(d, lang)
		}
	}
	r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: kind, Signature: sig, StartLine: line(n), EndLine: end, Complexity: cx})
}

func jsIsFunc(typ string) bool {
	switch typ {
	case "arrow_function", "function_expression", "function", "generator_function":
		return true
	}
	return false
}
