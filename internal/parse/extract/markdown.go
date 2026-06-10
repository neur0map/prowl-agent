package extract

import (
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

func init() { register(markdownExtractor{}) }

type markdownExtractor struct{}

func (markdownExtractor) Lang() string { return "markdown" }

// Headings become symbols so an agent can jump to a doc section by name; the
// block grammar leaves heading text as a single inline node. The full text is
// also chunked for full-text search.
const markdownSCM = `
(atx_heading heading_content: (inline) @atx.text) @atx.node
(setext_heading heading_content: (paragraph (inline) @setext.text)) @setext.node
`

func (markdownExtractor) Extract(src []byte) (Result, error) {
	var r Result
	err := queryEach("markdown", src, []byte(markdownSCM), func(caps []capture) {
		if t, ok := capNode(caps, "atx.text"); ok {
			if n, ok := capNode(caps, "atx.node"); ok {
				addHeading(&r, t.Content(src), atxLevel(n), n)
			}
		}
		if t, ok := capNode(caps, "setext.text"); ok {
			if n, ok := capNode(caps, "setext.node"); ok {
				addHeading(&r, t.Content(src), setextLevel(n), n)
			}
		}
	})
	r.Chunks = chunkText(src, 40)
	return r, err
}

func addHeading(r *Result, text, level string, node sitter.Node) {
	name := strings.TrimSpace(text)
	if name == "" {
		return
	}
	r.Symbols = append(r.Symbols, Symbol{
		Name: name, Kind: "heading", Signature: level,
		StartLine: line(node), EndLine: endLine(node),
	})
}

// atxLevel reads the depth from an atx_h1_marker..atx_h6_marker child.
func atxLevel(n sitter.Node) string {
	for i := uint32(0); i < n.NamedChildCount(); i++ {
		t := n.NamedChild(i).Type()
		if strings.HasPrefix(t, "atx_h") && strings.HasSuffix(t, "_marker") {
			return "h" + t[len("atx_h"):len("atx_h")+1]
		}
	}
	return "heading"
}

func setextLevel(n sitter.Node) string {
	for i := uint32(0); i < n.NamedChildCount(); i++ {
		switch n.NamedChild(i).Type() {
		case "setext_h1_underline":
			return "h1"
		case "setext_h2_underline":
			return "h2"
		}
	}
	return "heading"
}
