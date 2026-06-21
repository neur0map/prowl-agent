package extract

func init() {
	register(tsExtractor{lang: "typescript"})
	register(tsExtractor{lang: "tsx"})
}

// tsExtractor handles both TypeScript (.ts) and TSX (.tsx). The two grammars
// share node names, so one query and one extractor serve both; only the grammar
// id differs (tsx additionally parses JSX).
type tsExtractor struct{ lang string }

func (e tsExtractor) Lang() string { return e.lang }

// tsSCM captures named declarations worth finding by name (functions, classes,
// interfaces, type aliases, enums, methods), module-level const/var (so an
// arrow-function component like `export const App = () => ...` is found), and
// import sources as include edges. Class/interface/type names are
// type_identifier in TypeScript, unlike JavaScript's identifier.
const tsSCM = `
(function_declaration name: (identifier) @func.name) @func.def
(generator_function_declaration name: (identifier) @func.name) @func.def
(class_declaration name: (type_identifier) @class.name) @class.def
(abstract_class_declaration name: (type_identifier) @class.name) @class.def
(interface_declaration name: (type_identifier) @iface.name) @iface.def
(type_alias_declaration name: (type_identifier) @type.name) @type.def
(enum_declaration name: (identifier) @enum.name) @enum.def
(method_definition name: (property_identifier) @method.name) @method.def
(import_statement source: (string (string_fragment) @import.src))
(program (lexical_declaration (variable_declarator name: (identifier) @var.name value: (_) @var.value)))
(program (variable_declaration (variable_declarator name: (identifier) @var.name value: (_) @var.value)))
(program (export_statement declaration: (lexical_declaration (variable_declarator name: (identifier) @var.name value: (_) @var.value))))
(program (export_statement declaration: (variable_declaration (variable_declarator name: (identifier) @var.name value: (_) @var.value))))
`

func (e tsExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach(e.lang, src, []byte(tsSCM), func(caps []capture) {
		addNamed(&r, caps, src, "func.name", "func.def", "function", e.lang)
		addNamed(&r, caps, src, "class.name", "class.def", "class", e.lang)
		addNamed(&r, caps, src, "iface.name", "iface.def", "interface", e.lang)
		addNamed(&r, caps, src, "type.name", "type.def", "type", e.lang)
		addNamed(&r, caps, src, "enum.name", "enum.def", "enum", e.lang)
		addNamed(&r, caps, src, "method.name", "method.def", "method", e.lang)
		if n, ok := capNode(caps, "import.src"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
		if n, ok := capNode(caps, "var.name"); ok {
			kind, sig := "variable", ""
			end, cx := line(n), 0
			if v, ok := capNode(caps, "var.value"); ok {
				end = endLine(v)
				if jsIsFunc(v.Type()) {
					kind, cx, sig = "function", complexity(v, e.lang), signatureOf(v, src)
				}
			}
			r.Symbols = append(r.Symbols, Symbol{Name: n.Content(src), Kind: kind, Signature: sig, StartLine: line(n), EndLine: end, Complexity: cx})
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
