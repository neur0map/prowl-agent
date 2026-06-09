package extract

func init() { register(pythonExtractor{}) }

type pythonExtractor struct{}

func (pythonExtractor) Lang() string { return "python" }

const pythonSCM = `
(function_definition name: (identifier) @func.name) @func.def
(class_definition name: (identifier) @class.name) @class.def
(import_statement name: (dotted_name) @import.mod)
(import_from_statement module_name: (dotted_name) @import.mod)
`

func (pythonExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("python", src, []byte(pythonSCM), func(caps []capture) {
		if n, ok := capNode(caps, "func.name"); ok {
			end := line(n)
			if d, ok := capNode(caps, "func.def"); ok {
				end = endLine(d)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "function", StartLine: line(n), EndLine: end})
		}
		if n, ok := capNode(caps, "class.name"); ok {
			end := line(n)
			if d, ok := capNode(caps, "class.def"); ok {
				end = endLine(d)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "class", StartLine: line(n), EndLine: end})
		}
		if n, ok := capNode(caps, "import.mod"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
