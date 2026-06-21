package extract

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
			end, cx, sig := line(n), 1, ""
			if d, ok := capNode(caps, "func.def"); ok {
				end, cx, sig = endLine(d), complexity(d, "python"), signatureOf(d, src)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "function", Signature: sig, StartLine: line(n), EndLine: end, Complexity: cx})
		}
		if n, ok := capNode(caps, "class.name"); ok {
			end, sig := line(n), ""
			if d, ok := capNode(caps, "class.def"); ok {
				end, sig = endLine(d), signatureOf(d, src)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "class", Signature: sig, StartLine: line(n), EndLine: end})
		}
		if n, ok := capNode(caps, "import.mod"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
		if n, ok := capNode(caps, "import.rel"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
