package extract

import "strings"

func init() { register(bashExtractor{}) }

type bashExtractor struct{}

func (bashExtractor) Lang() string { return "bash" }

const bashSCM = `
(function_definition name: (word) @func.name) @func.def
(command name: (command_name (word) @cmd.name)) @cmd.node
`

func (bashExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("bash", src, []byte(bashSCM), func(caps []capture) {
		if n, ok := capNode(caps, "func.name"); ok {
			end := line(n)
			if d, ok := capNode(caps, "func.def"); ok {
				end = endLine(d)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "function", StartLine: line(n), EndLine: end})
		}
		if nm, ok := capNode(caps, "cmd.name"); ok {
			name := nm.Content(src)
			node, _ := capNode(caps, "cmd.node")
			switch {
			case name == "source" || name == ".":
				if arg, ok := firstChildOfType(node, "word"); ok {
					r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: arg.Content(src), Line: line(arg)})
				}
			case strings.Contains(name, "/") || strings.HasSuffix(name, ".sh"):
				r.Edges = append(r.Edges, RawEdge{Kind: "execs", Raw: name, Line: line(nm)})
			}
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
