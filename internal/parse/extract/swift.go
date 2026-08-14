package extract

func init() { register(swiftExtractor{}) }

type swiftExtractor struct{}

func (swiftExtractor) Lang() string { return "swift" }

// Swift has no file-level local imports: `import Foundation`, `import SwiftUI`,
// and `import <SPM module>` are module-level and stay informational (like Go
// stdlib). Local coupling in a Swift app is by TYPE USAGE -- a view instantiates
// another view, a service returns a model -- so every referenced capitalized
// type name is emitted as a `uses` edge that the resolver links to the file
// declaring that type (mirroring QML component instantiation). Declarations
// (struct/class/enum/actor/extension/protocol, functions, enum cases, and
// type-level properties -- a model's fields and a view's @State) become symbols.
//
// tree-sitter-swift models struct/class/enum/actor/extension all as
// class_declaration, split by the leading keyword token; an extension's name is
// a user_type, the others a bare type_identifier. protocol is its own node.
const swiftSCM = `
(import_declaration (identifier) @import.path)
(class_declaration "struct" name: (type_identifier) @struct.name) @struct.def
(class_declaration "class" name: (type_identifier) @class.name) @class.def
(class_declaration "enum" name: (type_identifier) @enum.name) @enum.def
(class_declaration "actor" name: (type_identifier) @actor.name) @actor.def
(class_declaration "extension" name: (user_type (type_identifier) @ext.name)) @ext.def
(protocol_declaration name: (type_identifier) @protocol.name) @protocol.def
(function_declaration name: (simple_identifier) @func.name) @func.def
(protocol_function_declaration name: (simple_identifier) @func.name) @func.def
(user_type (type_identifier) @use.type)
(call_expression (simple_identifier) @call.name)
(navigation_expression target: (simple_identifier) @nav.name)
(enum_class_body (enum_entry name: (simple_identifier) @case.name) @case.def)
(class_body (property_declaration name: (pattern bound_identifier: (simple_identifier) @prop.name)) @prop.def)
(enum_class_body (property_declaration name: (pattern bound_identifier: (simple_identifier) @prop.name)) @prop.def)
`

func (swiftExtractor) Extract(src []byte) (Result, error) {
	var r Result
	seenUse := map[string]bool{}
	err := queryEach("swift", src, []byte(swiftSCM), func(caps []capture) {
		addNamed(&r, caps, src, "struct.name", "struct.def", "struct", "swift")
		addNamed(&r, caps, src, "class.name", "class.def", "class", "swift")
		addNamed(&r, caps, src, "enum.name", "enum.def", "enum", "swift")
		addNamed(&r, caps, src, "actor.name", "actor.def", "class", "swift")
		addNamed(&r, caps, src, "ext.name", "ext.def", "extension", "swift")
		addNamed(&r, caps, src, "protocol.name", "protocol.def", "protocol", "swift")
		addNamed(&r, caps, src, "func.name", "func.def", "function", "swift")
		addNamed(&r, caps, src, "case.name", "case.def", "case", "swift")
		addNamed(&r, caps, src, "prop.name", "prop.def", "property", "swift")
		if n, ok := capNode(caps, "import.path"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
		// Capitalized type references become deduped `uses` edges. Built-in and
		// framework types (View, Text, VStack, Int) simply do not resolve.
		for _, capName := range []string{"use.type", "call.name", "nav.name"} {
			if n, ok := capNode(caps, capName); ok {
				name := n.Content(src)
				if name != "" && name[0] >= 'A' && name[0] <= 'Z' && !seenUse[name] {
					seenUse[name] = true
					r.Edges = append(r.Edges, RawEdge{Kind: "uses", Raw: name, Line: line(n)})
				}
			}
		}
	})
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, err
}
