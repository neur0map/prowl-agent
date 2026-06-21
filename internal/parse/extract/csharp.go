package extract

func init() { register(csharpExtractor{}) }

type csharpExtractor struct{}

func (csharpExtractor) Lang() string { return "csharp" }

const csharpSCM = `
(class_declaration name: (identifier) @class.name) @class.def
(interface_declaration name: (identifier) @iface.name) @iface.def
(struct_declaration name: (identifier) @struct.name) @struct.def
(record_declaration name: (identifier) @record.name) @record.def
(enum_declaration name: (identifier) @enum.name) @enum.def
(method_declaration name: (identifier) @method.name) @method.def
(using_directive (qualified_name) @using.path)
(using_directive (identifier) @using.path)
(namespace_declaration name: (qualified_name) @ns.name)
(namespace_declaration name: (identifier) @ns.name)
(file_scoped_namespace_declaration name: (qualified_name) @ns.name)
(file_scoped_namespace_declaration name: (identifier) @ns.name)
`

func (csharpExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("csharp", src, []byte(csharpSCM), func(caps []capture) {
		addNamed(&r, caps, src, "class.name", "class.def", "class", "csharp")
		addNamed(&r, caps, src, "iface.name", "iface.def", "interface", "csharp")
		addNamed(&r, caps, src, "struct.name", "struct.def", "struct", "csharp")
		addNamed(&r, caps, src, "record.name", "record.def", "record", "csharp")
		addNamed(&r, caps, src, "enum.name", "enum.def", "enum", "csharp")
		addNamed(&r, caps, src, "method.name", "method.def", "method", "csharp")
		if n, ok := capNode(caps, "using.path"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
		if n, ok := capNode(caps, "ns.name"); ok {
			r.Resources = append(r.Resources, Resource{Kind: "namespace", Name: n.Content(src), Line: line(n)})
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
