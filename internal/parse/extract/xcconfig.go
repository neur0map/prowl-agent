package extract

import (
	"regexp"
	"strings"
)

func init() { register(xcconfigExtractor{}) }

type xcconfigExtractor struct{}

func (xcconfigExtractor) Lang() string { return "xcconfig" }

var xcconfigKey = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_.]*)`)

// Extract reads an Xcode build-configuration file (.xcconfig): each `KEY = value`
// build setting becomes a setting symbol and each `#include` an include edge.
func (xcconfigExtractor) Extract(src []byte) (Result, error) {
	var r Result
	seen := map[string]bool{}
	for i, ln := range strings.Split(string(src), "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		if strings.HasPrefix(t, "#include") {
			inc := strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, "#include")), `"'?`)
			if inc != "" {
				r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: inc, Line: i + 1})
			}
			continue
		}
		if !strings.Contains(t, "=") {
			continue
		}
		if m := xcconfigKey.FindStringSubmatch(ln); m != nil {
			name := strings.TrimSpace(m[1])
			if name != "" && !seen[name] {
				seen[name] = true
				r.Symbols = append(r.Symbols, Symbol{Name: name, Kind: "setting", StartLine: i + 1, EndLine: i + 1})
			}
		}
	}
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, nil
}
