package extract

func init() { register(scssExtractor{}) }

type scssExtractor struct{}

func (scssExtractor) Lang() string { return "scss" }

const scssSCM = `
(declaration (variable_name) @var (color_value) @color)
(declaration (property_name) (variable_value) @use)
(declaration (property_name) (color_value) @hardcolor)
(import_statement (string_value) @import)
`

func (scssExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("scss", src, []byte(scssSCM), func(caps []capture) {
		if v, ok := capNode(caps, "var"); ok {
			val := ""
			if c, ok := capNode(caps, "color"); ok {
				val = c.Content(src)
			}
			name := v.Content(src)
			r.Resources = append(r.Resources, Resource{Kind: "color", Name: name, Value: val, Line: line(v)})
			r.Edges = append(r.Edges, RawEdge{Kind: "declares_resource", Raw: name, Line: line(v)})
		}
		if u, ok := capNode(caps, "use"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "uses_resource", Raw: u.Content(src), Line: line(u)})
		}
		if c, ok := capNode(caps, "hardcolor"); ok {
			r.Resources = append(r.Resources, Resource{Kind: "color", Value: c.Content(src), Line: line(c)})
		}
		if im, ok := capNode(caps, "import"); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: unquote(im.Content(src)), Line: line(im)})
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
