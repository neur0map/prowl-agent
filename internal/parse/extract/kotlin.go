package extract

import sitter "github.com/alexaandru/go-tree-sitter-bare"

func init() { register(kotlinExtractor{}) }

type kotlinExtractor struct{}

func (kotlinExtractor) Lang() string { return "kotlin" }

// kotlinSCM captures classes (and interfaces, which share the class_declaration
// node), objects, functions, and import edges. The grammar marks an enum with an
// enum_class_body and a wildcard import with a wildcard_import child, both
// inspected in Go. Imports are FQCNs (`com.foo.Bar`) resolved by the JVM pass.
const kotlinSCM = `
(class_declaration (type_identifier) @class.name) @class.def
(object_declaration (type_identifier) @object.name) @object.def
(function_declaration (simple_identifier) @func.name) @func.def
(import_header (identifier) @import.path) @import.hdr
`

func (kotlinExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("kotlin", src, []byte(kotlinSCM), func(caps []capture) {
		if n, ok := capNode(caps, "class.name"); ok {
			kind, end := "class", line(n)
			if d, ok := capNode(caps, "class.def"); ok {
				end = endLine(d)
				if hasChildType(d, "enum_class_body") {
					kind = "enum"
				}
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: kind, StartLine: line(n), EndLine: end})
		}
		if n, ok := capNode(caps, "object.name"); ok {
			end := line(n)
			if d, ok := capNode(caps, "object.def"); ok {
				end = endLine(d)
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "object", StartLine: line(n), EndLine: end})
		}
		addNamed(&r, caps, src, "func.name", "func.def", "function", "kotlin")
		if n, ok := capNode(caps, "import.path"); ok {
			raw := n.Content(src)
			if h, ok := capNode(caps, "import.hdr"); ok && hasChildType(h, "wildcard_import") {
				raw += ".*" // `import com.foo.*` -> matches the Java wildcard form
			}
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: raw, Line: line(n)})
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}

// hasChildType reports whether n has a named child of the given type.
func hasChildType(n sitter.Node, typ string) bool {
	for i := uint32(0); i < n.NamedChildCount(); i++ {
		if n.NamedChild(i).Type() == typ {
			return true
		}
	}
	return false
}
