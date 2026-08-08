package extract

func init() { register(rustExtractor{}) }

type rustExtractor struct{}

func (rustExtractor) Lang() string { return "rust" }

// rustSCM captures the named items worth finding (functions, structs, enums,
// traits, type aliases, modules, macros) and use paths as include edges. All
// function_items are treated as functions; Rust uses the same node for free
// functions and impl/trait methods, so they are not split.
const rustSCM = `
(function_item name: (identifier) @func.name) @func.def
(struct_item name: (type_identifier) @struct.name) @struct.def
(enum_item name: (type_identifier) @enum.name) @enum.def
(trait_item name: (type_identifier) @trait.name) @trait.def
(type_item name: (type_identifier) @type.name) @type.def
(mod_item name: (identifier) @mod.name) @mod.def
(macro_definition name: (identifier) @macro.name) @macro.def
(use_declaration argument: (_) @use.path)
`

func (rustExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("rust", src, []byte(rustSCM), func(caps []capture) {
		addNamed(&r, caps, src, "func.name", "func.def", "function", "rust")
		addNamed(&r, caps, src, "struct.name", "struct.def", "struct", "rust")
		addNamed(&r, caps, src, "enum.name", "enum.def", "enum", "rust")
		addNamed(&r, caps, src, "trait.name", "trait.def", "trait", "rust")
		addNamed(&r, caps, src, "type.name", "type.def", "type", "rust")
		addNamed(&r, caps, src, "mod.name", "mod.def", "module", "rust")
		addNamed(&r, caps, src, "macro.name", "macro.def", "macro", "rust")
		// `mod foo;` (no inline body) includes the file foo.rs / foo/mod.rs.
		if n, ok := capNode(caps, "mod.name"); ok {
			if def, ok2 := capNode(caps, "mod.def"); ok2 {
				if _, hasBody := firstChildOfType(def, "declaration_list"); !hasBody {
					r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: "mod::" + n.Content(src), Line: line(n)})
				}
			}
		}
		if n, ok := capNode(caps, "use.path"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
	})
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, err
}
