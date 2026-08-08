package extract

func init() { register(dartExtractor{}) }

type dartExtractor struct{}

func (dartExtractor) Lang() string { return "dart" }

// dartSCM captures classes, mixins, enums, extensions, top-level and member
// functions, and the URI of every import/export (under configurable_uri) and
// `part` directive. A `part of` directive is deliberately not captured: it is
// the reverse of the library's own `part`, so emitting it would fake a cycle
// between a file and its generated companion (.g.dart / .freezed.dart).
const dartSCM = `
(class_definition name: (identifier) @class.name) @class.def
(mixin_declaration (identifier) @mixin.name) @mixin.def
(enum_declaration name: (identifier) @enum.name) @enum.def
(extension_declaration name: (identifier) @ext.name) @ext.def
(function_signature name: (identifier) @func.name) @func.def
(configurable_uri (uri (string_literal) @import.uri))
(part_directive (uri (string_literal) @import.uri))
`

func (dartExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("dart", src, []byte(dartSCM), func(caps []capture) {
		addNamed(&r, caps, src, "class.name", "class.def", "class", "dart")
		addNamed(&r, caps, src, "mixin.name", "mixin.def", "mixin", "dart")
		addNamed(&r, caps, src, "enum.name", "enum.def", "enum", "dart")
		addNamed(&r, caps, src, "ext.name", "ext.def", "extension", "dart")
		if n, ok := capNode(caps, "func.name"); ok {
			// A function/method body is a sibling of the signature node, not a
			// child. For a method the signature is wrapped in method_signature, so
			// climb to it first; then the body is the next sibling.
			end, cx, sig := line(n), 1, ""
			if d, ok := capNode(caps, "func.def"); ok {
				end = endLine(d)
				sig = signatureOf(d, src) // function_signature is header-only here
				if p := d.Parent(); !p.IsNull() && p.Type() == "method_signature" {
					d = p
				}
				if body := d.NextNamedSibling(); !body.IsNull() && body.Type() == "function_body" {
					end = endLine(body)
					cx = complexity(body, "dart")
				}
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: "function", Signature: sig, StartLine: line(n), EndLine: end, Complexity: cx})
		}
		if n, ok := capNode(caps, "import.uri"); ok {
			if u := unquote(n.Content(src)); u != "" {
				r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: u, Line: line(n)})
			}
		}
	})
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, err
}
