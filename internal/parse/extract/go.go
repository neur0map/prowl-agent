package extract

func init() { register(goExtractor{}) }

type goExtractor struct{}

func (goExtractor) Lang() string { return "go" }

// goSCM captures the definitions worth searching by name (functions, methods,
// types) and import paths as include edges, mirroring the other language
// extractors. Call-graph edges are out of scope: Go symbol resolution is
// cross-file and package-aware, unlike the file-path includes prowl resolves.
const goSCM = `
(function_declaration name: (identifier) @func.name) @func.def
(method_declaration name: (field_identifier) @method.name) @method.def
(type_spec name: (type_identifier) @type.name) @type.def
(import_spec path: (interpreted_string_literal) @import.path)
`

func (goExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("go", src, []byte(goSCM), func(caps []capture) {
		if n, ok := capNode(caps, "func.name"); ok {
			end, cx, sig := line(n), 1, ""
			if d, ok := capNode(caps, "func.def"); ok {
				end, cx, sig = endLine(d), complexity(d, "go"), signatureOf(d, src)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "function", Signature: sig, StartLine: line(n), EndLine: end, Complexity: cx})
		}
		if n, ok := capNode(caps, "method.name"); ok {
			end, cx, sig := line(n), 1, ""
			if d, ok := capNode(caps, "method.def"); ok {
				end, cx, sig = endLine(d), complexity(d, "go"), signatureOf(d, src)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "method", Signature: sig, StartLine: line(n), EndLine: end, Complexity: cx})
		}
		if n, ok := capNode(caps, "type.name"); ok {
			end, sig := line(n), ""
			if d, ok := capNode(caps, "type.def"); ok {
				end, sig = endLine(d), signatureOf(d, src)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "type", Signature: sig, StartLine: line(n), EndLine: end})
		}
		if n, ok := capNode(caps, "import.path"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: unquote(n.Content(src)), Line: line(n)})
		}
	})
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, err
}
