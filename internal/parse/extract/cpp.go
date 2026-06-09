package extract

func init() { register(cppExtractor{}) }

type cppExtractor struct{}

func (cppExtractor) Lang() string { return "cpp" }

const cppSCM = `
(preproc_include path: (string_literal (string_content) @include.path))
(class_specifier name: (type_identifier) @class.name) @class.def
(function_definition declarator: (function_declarator declarator: (identifier) @func.name)) @func.def
(function_definition declarator: (function_declarator declarator: (qualified_identifier) @func.name)) @func.def
`

func (cppExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("cpp", src, []byte(cppSCM), func(caps []capture) {
		if p, ok := capNode(caps, "include.path"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: p.Content(src), Line: line(p)})
		}
		if n, ok := capNode(caps, "class.name"); ok {
			end := line(n)
			if d, ok := capNode(caps, "class.def"); ok {
				end = endLine(d)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "class", StartLine: line(n), EndLine: end})
		}
		if n, ok := capNode(caps, "func.name"); ok {
			end := line(n)
			if d, ok := capNode(caps, "func.def"); ok {
				end = endLine(d)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "function", StartLine: line(n), EndLine: end})
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
