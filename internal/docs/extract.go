package docs

import (
	"net/url"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// page is one extracted document.
type page struct {
	title    string
	markdown string
	links    []string // absolute, same-scheme URLs discovered in content + nav
}

// contentContainers are element ids/classes commonly wrapping documentation body
// content across Docusaurus, MkDocs, Sphinx, and Next.js docs sites.
var contentContainers = []string{
	"theme-doc-markdown", "markdown", "md-content", "document",
	"main-content", "content", "article-content", "doc-content", "prose",
}

// stripTags are structural chrome removed before conversion so the Markdown is
// the document body, not navigation, scripts, or styling.
var stripTags = map[atom.Atom]bool{
	atom.Nav: true, atom.Aside: true, atom.Footer: true, atom.Header: true,
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Form: true,
	atom.Svg: true, atom.Button: true,
}

// extractPage parses HTML, collects links, selects the main content, and
// converts it to Markdown. base resolves relative links.
func extractPage(rawHTML []byte, base *url.URL) (page, error) {
	root, err := html.Parse(strings.NewReader(string(rawHTML)))
	if err != nil {
		return page{}, err
	}
	var p page
	p.title = firstTitle(root)
	p.links = collectLinks(root, base)

	content := selectContent(root)
	if content == nil {
		return p, nil
	}
	stripChrome(content)
	md, err := htmltomarkdown.ConvertNode(content)
	if err != nil {
		return p, err
	}
	p.markdown = strings.TrimSpace(string(md))
	return p, nil
}

func firstTitle(root *html.Node) string {
	var title, h1 string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Title:
				if title == "" {
					title = strings.TrimSpace(textOf(n))
				}
			case atom.H1:
				if h1 == "" {
					h1 = strings.TrimSpace(textOf(n))
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if h1 != "" {
		return h1
	}
	return title
}

func collectLinks(root *html.Node, base *url.URL) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			for _, a := range n.Attr {
				if a.Key != "href" {
					continue
				}
				ref, err := url.Parse(strings.TrimSpace(a.Val))
				if err != nil {
					continue
				}
				abs := base.ResolveReference(ref)
				abs.Fragment = ""
				s := abs.String()
				if (abs.Scheme == "http" || abs.Scheme == "https") && !seen[s] {
					seen[s] = true
					out = append(out, s)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

// selectContent finds the most likely documentation body: <main>, then a known
// content container id/class, then <article>, then <body>.
func selectContent(root *html.Node) *html.Node {
	if n := firstElement(root, atom.Main); n != nil {
		return n
	}
	if n := firstContainer(root); n != nil {
		return n
	}
	if n := firstElement(root, atom.Article); n != nil {
		return n
	}
	return firstElement(root, atom.Body)
}

func firstElement(root *html.Node, a atom.Atom) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == a {
			found = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

func firstContainer(root *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode {
			id := attr(n, "id")
			class := attr(n, "class")
			for _, want := range contentContainers {
				if id == want || containsClass(class, want) {
					found = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

// stripChrome removes navigation/scripting/styling descendants in place.
func stripChrome(n *html.Node) {
	var next *html.Node
	for c := n.FirstChild; c != nil; c = next {
		next = c.NextSibling
		if c.Type == html.ElementNode && stripTags[c.DataAtom] {
			n.RemoveChild(c)
			continue
		}
		stripChrome(c)
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func containsClass(class, want string) bool {
	for _, f := range strings.Fields(class) {
		if f == want {
			return true
		}
	}
	return false
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
