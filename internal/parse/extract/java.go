package extract

func init() { register(javaExtractor{}) }

type javaExtractor struct{}

func (javaExtractor) Lang() string { return "java" }

const javaSCM = `
(class_declaration name: (identifier) @class.name) @class.def
(interface_declaration name: (identifier) @iface.name) @iface.def
(enum_declaration name: (identifier) @enum.name) @enum.def
(method_declaration name: (identifier) @method.name) @method.def
(import_declaration (scoped_identifier) @import.path)
`

func (javaExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("java", src, []byte(javaSCM), func(caps []capture) {
		addNamed(&r, caps, src, "class.name", "class.def", "class", "java")
		addNamed(&r, caps, src, "iface.name", "iface.def", "interface", "java")
		addNamed(&r, caps, src, "enum.name", "enum.def", "enum", "java")
		addNamed(&r, caps, src, "method.name", "method.def", "method", "java")
		if n, ok := capNode(caps, "import.path"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
	})
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, err
}
