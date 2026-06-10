package extract

func init() { register(qmlExtractor{}) }

type qmlExtractor struct{}

func (qmlExtractor) Lang() string { return "qml" }

const qmlSCM = `
(ui_import source: (identifier) @import.name)
(ui_object_definition type_name: (identifier) @comp.name)
(ui_binding name: (identifier) @bind.name value: (expression_statement (identifier) @bind.val))
(ui_property name: (identifier) @prop.name)
`

// Extract turns a QML file into edges and symbols. The dominant structure in QML
// is component instantiation: a file `Foo.qml` defines component `Foo`, and other
// files use it as `Foo { ... }`. Those usages are emitted as `instantiates` edges
// (deduped per type) so the resolver can link them to the defining `.qml` file by
// name; built-in QtQuick/Quickshell types simply do not resolve. Imports are kept
// as `includes` edges (mostly external modules). The file's own component symbol
// (filename-derived) is added by the indexer, which knows the path.
func (qmlExtractor) Extract(src []byte) (Result, error) {
	var r Result
	seen := map[string]bool{}
	err := queryEach("qml", src, []byte(qmlSCM), func(caps []capture) {
		if n, ok := capNode(caps, "import.name"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
		if n, ok := capNode(caps, "comp.name"); ok {
			name := n.Content(src)
			if !seen[name] {
				seen[name] = true
				r.Edges = append(r.Edges, RawEdge{Kind: "instantiates", Raw: name, Line: line(n)})
			}
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
