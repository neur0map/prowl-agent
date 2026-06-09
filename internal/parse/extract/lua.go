package extract

func init() { register(luaExtractor{}) }

type luaExtractor struct{}

func (luaExtractor) Lang() string { return "lua" }

const luaSCM = `
(function_declaration name: (identifier) @func.name) @func.def
(function_declaration name: (dot_index_expression) @func.name) @func.def
(function_call
  name: (identifier) @_req
  arguments: (arguments (string content: (string_content) @require.path))
  (#eq? @_req "require"))
`

func (luaExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("lua", src, []byte(luaSCM), func(caps []capture) {
		if n, ok := capNode(caps, "func.name"); ok {
			end := line(n)
			if d, ok := capNode(caps, "func.def"); ok {
				end = endLine(d)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "function", StartLine: line(n), EndLine: end})
		}
		if p, ok := capNode(caps, "require.path"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: p.Content(src), Line: line(p)})
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
