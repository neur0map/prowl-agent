package extract

import (
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

func init() { register(elixirExtractor{}) }

type elixirExtractor struct{}

func (elixirExtractor) Lang() string { return "elixir" }

// elixirSCM captures every call with an identifier target and an argument list.
// Elixir has no dedicated keyword nodes -- defmodule, def, alias, import, use are
// all macro calls -- so the extractor dispatches on the target identifier's text.
// A defmodule's name is recorded as a namespace (like C#), so alias/import/use of
// that module resolve to its file; def/defp/defmacro become function symbols.
const elixirSCM = `(call target: (identifier) @kw (arguments) @args) @call`

func (elixirExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("elixir", src, []byte(elixirSCM), func(caps []capture) {
		kw, ok := capNode(caps, "kw")
		if !ok {
			return
		}
		args, ok := capNode(caps, "args")
		if !ok {
			return
		}
		call, _ := capNode(caps, "call")
		switch kw.Content(src) {
		case "defmodule":
			if name := firstAlias(args, src); name != "" {
				r.Resources = append(r.Resources, Resource{Kind: "namespace", Name: name, Line: line(kw)})
				r.Symbols = append(r.Symbols, Symbol{Name: name, Kind: "module", StartLine: line(kw), EndLine: endLine(call), Signature: "defmodule " + name})
			}
		case "def", "defp", "defmacro", "defmacrop":
			if name := elixirDefName(args, src); name != "" {
				r.Symbols = append(r.Symbols, Symbol{
					Name: name, Kind: "function", StartLine: line(kw), EndLine: endLine(call),
					Signature: elixirSignature(call, src), Complexity: complexity(call, "elixir"),
				})
			}
		case "alias", "import", "use", "require":
			for _, raw := range elixirAliasTargets(args, src) {
				r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: raw, Line: line(kw)})
			}
		}
	})
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, err
}

// firstAlias returns the dotted name of the first alias argument (a module name).
func firstAlias(args sitter.Node, src []byte) string {
	for i := uint32(0); i < args.NamedChildCount(); i++ {
		if c := args.NamedChild(i); c.Type() == "alias" {
			return c.Content(src)
		}
	}
	return ""
}

// elixirDefName returns the function name from a def's first argument, which is
// either a call (`greet(name)`) whose target is the name, or a bare identifier
// (`greet` with no parens).
func elixirDefName(args sitter.Node, src []byte) string {
	if args.NamedChildCount() == 0 {
		return ""
	}
	switch first := args.NamedChild(0); first.Type() {
	case "call":
		if t := first.ChildByFieldName("target"); !t.IsNull() && t.Type() == "identifier" {
			return t.Content(src)
		}
	case "identifier":
		return first.Content(src)
	}
	return ""
}

// elixirAliasTargets returns the module names referenced by an alias/import/use,
// expanding the group form `alias A.{B, C}` into A.B and A.C.
func elixirAliasTargets(args sitter.Node, src []byte) []string {
	var out []string
	for i := uint32(0); i < args.NamedChildCount(); i++ {
		c := args.NamedChild(i)
		switch c.Type() {
		case "alias":
			out = append(out, c.Content(src))
		case "dot":
			left, right := c.ChildByFieldName("left"), c.ChildByFieldName("right")
			if !left.IsNull() && left.Type() == "alias" && !right.IsNull() && right.Type() == "tuple" {
				prefix := left.Content(src)
				for j := uint32(0); j < right.NamedChildCount(); j++ {
					if t := right.NamedChild(j); t.Type() == "alias" {
						out = append(out, prefix+"."+t.Content(src))
					}
				}
			}
		}
	}
	return out
}

// elixirSignature is the def's first line (its head), trimmed of a trailing
// `do`, so find shows the head without the body.
func elixirSignature(call sitter.Node, src []byte) string {
	s := call.Content(src)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return clipSignature(strings.TrimSuffix(strings.TrimSpace(s), " do"))
}
