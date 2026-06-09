package extract

func init() { register(fishExtractor{}) }

type fishExtractor struct{}

func (fishExtractor) Lang() string { return "fish" }

const fishSCM = `
(function_definition name: (word) @func.name) @func.def
(command name: (word) @cmd.name) @cmd.node
`

func (fishExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("fish", src, []byte(fishSCM), func(caps []capture) {
		if n, ok := capNode(caps, "func.name"); ok {
			end := line(n)
			if d, ok := capNode(caps, "func.def"); ok {
				end = endLine(d)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "function", StartLine: line(n), EndLine: end})
		}
		if nm, ok := capNode(caps, "cmd.name"); ok {
			name := nm.Content(src)
			if name != "source" && name != "." {
				return
			}
			if node, ok := capNode(caps, "cmd.node"); ok && node.NamedChildCount() >= 2 {
				arg := node.NamedChild(1)
				r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: unquote(arg.Content(src)), Line: line(arg)})
			}
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
