package sketch

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// SingletonSource returns the QML source of a singleton by component name
// (e.g. "Tokens" -> the bytes of Tokens.qml), or ok=false if none is known.
type SingletonSource func(name string) ([]byte, bool)

// refPattern matches a single qualified reference to a singleton property, like
// `Tokens.ink` or `Theme.brand`: an uppercase-initial singleton name and one
// property segment. Multi-segment (`cc.root.bg`) and lowercase-rooted
// (`parent.width`) values are not singleton tokens and are left untouched.
var refPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*$`)

// Resolve annotates property values that reference a singleton token with the
// literal they point to (Tokens.ink -> #cdd6f4), fetching singleton sources via
// src and following alias chains a few hops. It is best-effort: a reference to
// an unknown singleton or an enum (Font.Medium) is left unresolved.
func (sk *Sketch) Resolve(src SingletonSource) {
	if src == nil || sk == nil {
		return
	}
	r := &resolver{src: src, cache: map[string]map[string]string{}}
	for _, root := range append([]*Node{sk.Root}, sk.Variants...) {
		if root != nil {
			resolveNode(root, r)
		}
	}
}

type resolver struct {
	src   SingletonSource
	cache map[string]map[string]string // singleton name -> prop -> raw value
}

func resolveNode(n *Node, r *resolver) {
	for i := range n.Props {
		if v, ok := r.resolve(n.Props[i].Value, 0); ok {
			n.Props[i].Resolved = v
		}
	}
	for _, c := range n.Children {
		resolveNode(c, r)
	}
}

// resolve turns a token reference into its literal value, following alias chains
// (Tokens.ink -> Theme.text -> "#cdd6f4") up to a small depth.
func (r *resolver) resolve(value string, depth int) (string, bool) {
	if depth > 4 || !refPattern.MatchString(value) {
		return "", false
	}
	name, prop, _ := strings.Cut(value, ".")
	vals := r.load(name)
	raw, ok := vals[prop]
	if !ok {
		return "", false
	}
	raw = strings.Trim(raw, `"`)
	if refPattern.MatchString(raw) {
		if chained, ok := r.resolve(raw, depth+1); ok {
			return chained, true
		}
	}
	return raw, true
}

func (r *resolver) load(name string) map[string]string {
	if vals, ok := r.cache[name]; ok {
		return vals
	}
	vals := map[string]string{}
	if src, ok := r.src(name); ok {
		vals = SingletonValues(src)
	}
	r.cache[name] = vals
	return vals
}

// SingletonValues parses a QML singleton's source into a map of declared
// property name to its source value (quotes not stripped).
func SingletonValues(src []byte) map[string]string {
	sk, err := extractQML("singleton.qml", src)
	if err != nil || sk.Root == nil {
		return nil
	}
	vals := make(map[string]string, len(sk.Root.Decls))
	for _, d := range sk.Root.Decls {
		if d.Value != "" {
			vals[d.Name] = d.Value
		}
	}
	return vals
}

// DirSingletonSource builds a SingletonSource that finds `<name>.qml` files
// under root. When several files share a basename, it prefers the one whose
// path is closest to near (longest shared directory prefix), so a component
// resolves against the theme that ships alongside it. The directory is walked
// once, lazily, on first lookup.
func DirSingletonSource(root, near string) SingletonSource {
	var once sync.Once
	index := map[string][]string{}
	build := func() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, ".qml") {
				name := strings.TrimSuffix(filepath.Base(p), ".qml")
				index[name] = append(index[name], p)
			}
			return nil
		})
	}
	return func(name string) ([]byte, bool) {
		once.Do(build)
		cands := index[name]
		if len(cands) == 0 {
			return nil, false
		}
		best := cands[0]
		for _, c := range cands[1:] {
			if sharedPrefixLen(c, near) > sharedPrefixLen(best, near) {
				best = c
			}
		}
		data, err := os.ReadFile(best)
		if err != nil {
			return nil, false
		}
		return data, true
	}
}

func sharedPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}
