package extract

func init() { register(rubyExtractor{}) }

type rubyExtractor struct{}

func (rubyExtractor) Lang() string { return "ruby" }

const rubySCM = `
(method name: (identifier) @method.name) @method.def
(singleton_method name: (identifier) @method.name) @method.def
(class name: (constant) @class.name) @class.def
(module name: (constant) @module.name) @module.def
(call method: (identifier) @req.fn arguments: (argument_list (string (string_content) @req.arg)))
`

func (rubyExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("ruby", src, []byte(rubySCM), func(caps []capture) {
		if n, ok := capNode(caps, "method.name"); ok {
			end, cx, sig := line(n), 1, ""
			if d, ok := capNode(caps, "method.def"); ok {
				end, cx, sig = endLine(d), complexity(d, "ruby"), signatureOf(d, src)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "method", Signature: sig, StartLine: line(n), EndLine: end, Complexity: cx})
		}
		addNamed(&r, caps, src, "class.name", "class.def", "class", "ruby")
		addNamed(&r, caps, src, "module.name", "module.def", "module", "ruby")
		if fn, ok := capNode(caps, "req.fn"); ok {
			if name := fn.Content(src); name == "require" || name == "require_relative" {
				if a, ok := capNode(caps, "req.arg"); ok {
					r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: a.Content(src), Line: line(a)})
				}
			}
		}
	})
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, err
}
