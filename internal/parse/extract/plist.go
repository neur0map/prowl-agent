package extract

import (
	"regexp"
	"strings"
)

func init() { register(plistExtractor{}) }

type plistExtractor struct{}

func (plistExtractor) Lang() string { return "plist" }

var plistKey = regexp.MustCompile(`<key>([^<]+)</key>`)

// Extract reads an XML property list (Info.plist, .entitlements): each <key>
// becomes a setting symbol, so bundle ids, capabilities, and usage descriptions
// are findable and searchable without opening the file. It is a plain line scan;
// plist has no Tree-sitter grammar.
func (plistExtractor) Extract(src []byte) (Result, error) {
	var r Result
	seen := map[string]bool{}
	for i, ln := range strings.Split(string(src), "\n") {
		for _, m := range plistKey.FindAllStringSubmatch(ln, -1) {
			name := strings.TrimSpace(m[1])
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			r.Symbols = append(r.Symbols, Symbol{Name: name, Kind: "setting", StartLine: i + 1, EndLine: i + 1})
		}
	}
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, nil
}
