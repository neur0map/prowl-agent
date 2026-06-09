package extract

import "strings"

func init() { register(cssExtractor{}) }

type cssExtractor struct{}

func (cssExtractor) Lang() string { return "css" }

const cssSCM = `
(declaration (property_name) @prop (color_value) @color)
(call_expression (function_name) @fn (arguments (plain_value) @arg))
(import_statement (string_value (string_content) @import.path))
`

func (cssExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("css", src, []byte(cssSCM), func(caps []capture) {
		if prop, ok := capNode(caps, "prop"); ok {
			if color, ok := capNode(caps, "color"); ok {
				name, val := prop.Content(src), color.Content(src)
				if strings.HasPrefix(name, "--") {
					r.Resources = append(r.Resources, Resource{Kind: "color", Name: name, Value: val, Line: line(prop)})
					r.Edges = append(r.Edges, RawEdge{Kind: "declares_resource", Raw: name, Line: line(prop)})
				} else {
					r.Resources = append(r.Resources, Resource{Kind: "color", Value: val, Line: line(color)})
				}
			}
		}
		if fn, ok := capNode(caps, "fn"); ok {
			if arg, ok := capNode(caps, "arg"); ok && fn.Content(src) == "var" {
				r.Edges = append(r.Edges, RawEdge{Kind: "uses_resource", Raw: arg.Content(src), Line: line(arg)})
			}
		}
		if p, ok := capNode(caps, "import.path"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: p.Content(src), Line: line(p)})
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
