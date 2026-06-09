package extract

import "strings"

func init() { register(hyprlangExtractor{}) }

type hyprlangExtractor struct{}

func (hyprlangExtractor) Lang() string { return "hyprlang" }

const hyprSCM = `
(declaration (variable) @var.name) @decl
(section (name) @section.name)
(assignment (name) @assign.name) @assign
(source (string) @source.path)
(keyword (name) @kw.name (params) @kw.params) @kw
(exec (string) @exec.cmd)
`

func (hyprlangExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("hyprlang", src, []byte(hyprSCM), func(caps []capture) {
		if v, ok := capNode(caps, "var.name"); ok {
			val := ""
			if d, ok := capNode(caps, "decl"); ok {
				val = valueAfterEq(d.Content(src))
			}
			name := v.Content(src)
			r.Resources = append(r.Resources, Resource{Kind: classifyResource(val), Name: name, Value: val, Line: line(v)})
			r.Edges = append(r.Edges, RawEdge{Kind: "declares_resource", Raw: name, Line: line(v)})
		}
		if s, ok := capNode(caps, "section.name"); ok {
			r.Symbols = append(r.Symbols, Symbol{Name: s.Content(src), Kind: "config_section", StartLine: line(s), EndLine: line(s)})
		}
		if a, ok := capNode(caps, "assign.name"); ok {
			val := ""
			if an, ok := capNode(caps, "assign"); ok {
				val = valueAfterEq(an.Content(src))
			}
			r.Symbols = append(r.Symbols, Symbol{Name: a.Content(src), Kind: "setting", Signature: val, StartLine: line(a), EndLine: line(a)})
			if looksLikeColor(val) {
				r.Resources = append(r.Resources, Resource{Kind: "color", Value: val, Line: line(a)})
			}
			if strings.HasPrefix(val, "$") {
				r.Edges = append(r.Edges, RawEdge{Kind: "uses_resource", Raw: val, Line: line(a)})
			}
		}
		if p, ok := capNode(caps, "source.path"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: unquote(p.Content(src)), Line: line(p)})
		}
		if kw, ok := capNode(caps, "kw.name"); ok && strings.HasPrefix(kw.Content(src), "bind") {
			if params, ok := capNode(caps, "kw.params"); ok {
				fields := splitCSV(params.Content(src))
				name := "bind"
				if len(fields) >= 2 {
					name = fields[0] + " " + fields[1]
				}
				r.Symbols = append(r.Symbols, Symbol{Name: name, Kind: "keybind", Signature: params.Content(src), StartLine: line(kw), EndLine: line(kw)})
				if len(fields) >= 4 && fields[2] == "exec" {
					r.Edges = append(r.Edges, RawEdge{SrcName: name, Kind: "binds", Raw: strings.Join(fields[3:], ", "), Line: line(kw)})
				}
			}
		}
		if c, ok := capNode(caps, "exec.cmd"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "execs", Raw: unquote(c.Content(src)), Line: line(c)})
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
