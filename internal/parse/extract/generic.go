package extract

import (
	"bufio"
	"bytes"
	"strings"
)

// genericExtractor is the line-oriented fallback for bespoke WM config formats
// without a Tree-sitter grammar (sway/i3 config, rofi rasi, polybar, dunst...).
func init() {
	register(genericExtractor{lang: "generic"})
	register(genericExtractor{lang: "rasi"})
}

type genericExtractor struct{ lang string }

func (e genericExtractor) Lang() string { return e.lang }

func (e genericExtractor) Extract(src []byte) (Result, error) {
	var r Result
	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	ln := 0
	for sc.Scan() {
		ln++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
			continue
		}
		if p, ok := matchInclude(line); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "includes", Raw: p, Line: ln})
			continue
		}
		if cmd, ok := matchExec(line); ok {
			r.Edges = append(r.Edges, RawEdge{Kind: "execs", Raw: cmd, Line: ln})
			continue
		}
		if kb, cmd, ok := matchBind(line); ok {
			r.Symbols = append(r.Symbols, Symbol{Name: kb, Kind: "keybind", Signature: line, StartLine: ln, EndLine: ln})
			if cmd != "" {
				r.Edges = append(r.Edges, RawEdge{SrcName: kb, Kind: "binds", Raw: cmd, Line: ln})
			}
			continue
		}
		if name, val, ok := matchVarDecl(line); ok {
			r.Resources = append(r.Resources, Resource{Kind: classifyResource(val), Name: name, Value: val, Line: ln})
			r.Edges = append(r.Edges, RawEdge{Kind: "declares_resource", Raw: name, Line: ln})
			continue
		}
		if key, val, ok := matchSetting(line); ok {
			r.Symbols = append(r.Symbols, Symbol{Name: key, Kind: "setting", Signature: val, StartLine: ln, EndLine: ln})
			switch {
			case looksLikeColor(val):
				r.Resources = append(r.Resources, Resource{Kind: "color", Value: val, Line: ln})
			case looksLikePath(val):
				r.Edges = append(r.Edges, RawEdge{Kind: "references", Raw: val, Line: ln})
			case strings.HasPrefix(val, "$") || strings.HasPrefix(val, "@"):
				r.Edges = append(r.Edges, RawEdge{Kind: "uses_resource", Raw: strings.TrimRight(val, ";"), Line: ln})
			}
		}
	}
	r.Chunks = chunkText(src, 40)
	return r, sc.Err()
}

func matchInclude(line string) (string, bool) {
	for _, p := range []string{"include ", "include=", "source ", "source="} {
		if strings.HasPrefix(line, p) {
			return strings.TrimSpace(strings.TrimPrefix(line, p)), true
		}
	}
	if strings.HasPrefix(line, "@import") {
		return unquote(strings.TrimSpace(strings.TrimPrefix(line, "@import"))), true
	}
	return "", false
}

func matchExec(line string) (string, bool) {
	for _, p := range []string{"exec_always ", "exec-always ", "exec-once ", "exec_once ", "exec "} {
		if strings.HasPrefix(line, p) {
			cmd := strings.TrimSpace(strings.TrimPrefix(line, p))
			cmd = strings.TrimPrefix(cmd, "--no-startup-id ")
			return strings.TrimSpace(cmd), true
		}
	}
	return "", false
}

func matchBind(line string) (kb, cmd string, ok bool) {
	for _, p := range []string{"bindsym ", "bindcode ", "bind ", "bind="} {
		if strings.HasPrefix(line, p) {
			rest := strings.TrimSpace(strings.TrimPrefix(line, p))
			if i := strings.Index(rest, " exec "); i >= 0 {
				return strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i+len(" exec "):]), true
			}
			f := strings.SplitN(rest, " ", 2)
			return f[0], "", true
		}
	}
	return "", "", false
}

func matchVarDecl(line string) (name, val string, ok bool) {
	if strings.HasPrefix(line, "set ") {
		rest := strings.TrimSpace(strings.TrimPrefix(line, "set "))
		f := strings.SplitN(rest, " ", 2)
		if len(f) == 2 && strings.HasPrefix(f[0], "$") {
			return f[0], strings.TrimSpace(f[1]), true
		}
	}
	if strings.HasPrefix(line, "$") {
		if i := strings.IndexByte(line, '='); i >= 0 {
			return strings.TrimSpace(line[:i]), strings.TrimSpace(strings.TrimRight(line[i+1:], ";")), true
		}
	}
	return "", "", false
}

func matchSetting(line string) (key, val string, ok bool) {
	var idx int
	switch {
	case strings.ContainsRune(line, '='):
		idx = strings.IndexByte(line, '=')
	case strings.ContainsRune(line, ':'):
		idx = strings.IndexByte(line, ':')
	default:
		f := strings.SplitN(line, " ", 2)
		if len(f) == 2 && isIdent(f[0]) {
			return f[0], strings.TrimSpace(f[1]), true
		}
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	val = strings.TrimSpace(strings.TrimRight(line[idx+1:], ";"))
	if key == "" || strings.ContainsAny(key, " \t{}") {
		return "", "", false
	}
	return key, val, true
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c == '-' || c == '_' || c == '.':
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}
