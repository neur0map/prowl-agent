package extract

func init() { register(qmlExtractor{}) }

type qmlExtractor struct{}

func (qmlExtractor) Lang() string { return "qml" }

const qmlSCM = `
(ui_import source: (identifier) @import.name)
(ui_object_definition type_name: (identifier) @comp.name) @comp.def
(ui_binding name: (identifier) @bind.name value: (expression_statement (identifier) @bind.val))
(ui_property name: (identifier) @prop.name)
`

func (qmlExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("qml", src, []byte(qmlSCM), func(caps []capture) {
		if n, ok := capNode(caps, "import.name"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
		if n, ok := capNode(caps, "comp.name"); ok {
			end := line(n)
			if d, ok := capNode(caps, "comp.def"); ok {
				end = endLine(d)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "component", StartLine: line(n), EndLine: end})
		}
		if bn, ok := capNode(caps, "bind.name"); ok && bn.Content(src) == "id" {
			if bv, ok := capNode(caps, "bind.val"); ok {
				r.Symbols = append(r.Symbols, Symbol{Name: bv.Content(src), Kind: "qml_id", StartLine: line(bv), EndLine: line(bv)})
			}
		}
		if n, ok := capNode(caps, "prop.name"); ok {
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "property", StartLine: line(n), EndLine: line(n)})
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
