package extract

import "strings"

// structured handles key/value config formats with Tree-sitter grammars.
// Each query uses the standardized captures @section, @key, @val so one handler
// serves all four formats.
func init() {
	for _, lang := range []string{"json", "yaml", "toml", "ini"} {
		register(structuredExtractor{lang: lang, scm: structuredSCM[lang]})
	}
}

var structuredSCM = map[string]string{
	"json": `
(pair key: (string (string_content) @key))
(pair value: (string (string_content) @val))
`,
	"yaml": `
(block_mapping_pair key: (flow_node (plain_scalar (string_scalar) @key)))
(block_mapping_pair value: (flow_node (plain_scalar (string_scalar) @val)))
`,
	"toml": `
(table (bare_key) @section)
(pair (bare_key) @key)
(pair (bare_key) (string) @val)
`,
	"ini": `
(section (section_name (text) @section))
(setting (setting_name) @key)
(setting (setting_name) (setting_value) @val)
`,
}

type structuredExtractor struct {
	lang string
	scm  string
}

func (e structuredExtractor) Lang() string { return e.lang }

func (e structuredExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach(e.lang, src, []byte(e.scm), func(caps []capture) {
		if s, ok := capNode(caps, "section"); ok {
			name := strings.TrimSpace(s.Content(src))
			r.Symbols = append(r.Symbols, Symbol{Name: name, Kind: "config_section", StartLine: line(s), EndLine: line(s)})
		}
		if k, ok := capNode(caps, "key"); ok {
			name := strings.TrimSpace(k.Content(src))
			r.Symbols = append(r.Symbols, Symbol{Name: name, Kind: "setting", StartLine: line(k), EndLine: line(k)})
		}
		if v, ok := capNode(caps, "val"); ok {
			raw := unquote(v.Content(src))
			if looksLikePath(raw) {
				r.Edges = append(r.Edges, RawEdge{Kind: "references", Raw: raw, Line: line(v)})
			}
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}
