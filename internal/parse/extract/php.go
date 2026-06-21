package extract

import sitter "github.com/alexaandru/go-tree-sitter-bare"

func init() { register(phpExtractor{}) }

type phpExtractor struct{}

func (phpExtractor) Lang() string { return "php" }

// phpSCM captures definitions (classes, interfaces, traits, enums, functions,
// methods), the file's namespace, and import edges. A `use Ns\Class` import is a
// fully-qualified class name (resolved to the declaring file by the namespace
// pass); a require/include is a path literal (resolved as a path). Both kinds are
// recorded as include edges.
const phpSCM = `
(namespace_definition name: (namespace_name) @ns.name)
(class_declaration name: (name) @class.name) @class.def
(interface_declaration name: (name) @iface.name) @iface.def
(trait_declaration name: (name) @trait.name) @trait.def
(enum_declaration name: (name) @enum.name) @enum.def
(function_definition name: (name) @func.name) @func.def
(method_declaration name: (name) @method.name) @method.def
(namespace_use_clause (qualified_name) @use.path)
(namespace_use_declaration (namespace_name) @group.prefix body: (namespace_use_group (namespace_use_clause (name) @group.name)))
(require_expression) @req.expr
(require_once_expression) @req.expr
(include_expression) @req.expr
(include_once_expression) @req.expr
`

func (phpExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("php", src, []byte(phpSCM), func(caps []capture) {
		addNamed(&r, caps, src, "class.name", "class.def", "class", "php")
		addNamed(&r, caps, src, "iface.name", "iface.def", "interface", "php")
		addNamed(&r, caps, src, "trait.name", "trait.def", "trait", "php")
		addNamed(&r, caps, src, "enum.name", "enum.def", "enum", "php")
		addNamed(&r, caps, src, "func.name", "func.def", "function", "php")
		addNamed(&r, caps, src, "method.name", "method.def", "method", "php")
		if n, ok := capNode(caps, "ns.name"); ok {
			r.Resources = append(r.Resources, Resource{Kind: "namespace", Name: n.Content(src), Line: line(n)})
		}
		if n, ok := capNode(caps, "use.path"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: n.Content(src), Line: line(n)})
		}
		// Group use: `use App\Model\{User, Post};` -> one FQCN edge per member.
		if p, ok := capNode(caps, "group.prefix"); ok {
			if nm, ok := capNode(caps, "group.name"); ok {
				r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: p.Content(src) + "\\" + nm.Content(src), Line: line(nm)})
			}
		}
		// require/include: take the path string even when concatenated with __DIR__.
		if e, ok := capNode(caps, "req.expr"); ok {
			if s := firstStringContent(e, src); s != "" {
				r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: s, Line: line(e)})
			}
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}

// firstStringContent returns the first string_content descendant's text, used to
// pull the path out of a require/include whose argument is a plain or
// concatenated string literal.
func firstStringContent(n sitter.Node, src []byte) string {
	var found string
	var walk func(sitter.Node)
	walk = func(node sitter.Node) {
		if found != "" {
			return
		}
		if node.Type() == "string_content" {
			found = node.Content(src)
			return
		}
		for i := uint32(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(n)
	return found
}
