package extract

import (
	"regexp"
	"strings"
)

func init() { register(pbxprojExtractor{}) }

type pbxprojExtractor struct{}

func (pbxprojExtractor) Lang() string { return "pbxproj" }

var (
	pbxTarget  = regexp.MustCompile(`productName = "?([^";]+)"?;`)
	pbxSetting = regexp.MustCompile(`^\s*([A-Z][A-Z0-9_]+) = `)
)

// Extract reads an Xcode project file (project.pbxproj): each build target
// (productName) is a `target` symbol and each ALL_CAPS build setting a `setting`
// symbol, so targets and configuration (bundle id, Swift version, team) are
// findable without reading the file. pbxproj is a NeXTSTEP plist with no
// Tree-sitter grammar, so this is a focused line scan.
func (pbxprojExtractor) Extract(src []byte) (Result, error) {
	var r Result
	seen := map[string]bool{}
	for i, ln := range strings.Split(string(src), "\n") {
		if m := pbxTarget.FindStringSubmatch(ln); m != nil {
			name := strings.TrimSpace(m[1])
			if name != "" && !seen["t:"+name] {
				seen["t:"+name] = true
				r.Symbols = append(r.Symbols, Symbol{Name: name, Kind: "target", StartLine: i + 1, EndLine: i + 1})
			}
			continue
		}
		if m := pbxSetting.FindStringSubmatch(ln); m != nil {
			name := m[1]
			if !seen["s:"+name] {
				seen["s:"+name] = true
				r.Symbols = append(r.Symbols, Symbol{Name: name, Kind: "setting", StartLine: i + 1, EndLine: i + 1})
			}
		}
	}
	r.Chunks = chunkStructured(src, r.Symbols, 40)
	return r, nil
}
