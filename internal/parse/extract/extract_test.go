package extract

import "testing"

func symNames(r Result, kind string) []string {
	var out []string
	for _, s := range r.Symbols {
		if s.Kind == kind {
			out = append(out, s.Name)
		}
	}
	return out
}

func edgeRaws(r Result, kind string) []string {
	var out []string
	for _, e := range r.Edges {
		if e.Kind == kind {
			out = append(out, e.Raw)
		}
	}
	return out
}

func has(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func mustExtract(t *testing.T, lang, src string) Result {
	t.Helper()
	e, ok := For(lang)
	if !ok {
		t.Fatalf("no extractor for %s", lang)
	}
	r, err := e.Extract([]byte(src))
	if err != nil {
		t.Fatalf("%s extract: %v", lang, err)
	}
	return r
}

func TestLuaExtractor(t *testing.T) {
	r := mustExtract(t, "lua", "local function helper()\nend\nfunction M.setup(o)\n  require(\"foo.bar\")\nend\nprint(\"hi\")\n")
	fn := symNames(r, "function")
	if !has(fn, "helper") || !has(fn, "M.setup") {
		t.Fatalf("functions=%v", fn)
	}
	inc := edgeRaws(r, "includes")
	if len(inc) != 1 || inc[0] != "foo.bar" {
		t.Fatalf("includes=%v (require predicate must exclude print)", inc)
	}
}

func TestBashExtractor(t *testing.T) {
	r := mustExtract(t, "bash", "#!/bin/bash\nfoo() {\n  echo hi\n}\nsource ./lib.sh\n~/.config/x/y.sh --flag\n")
	if !has(symNames(r, "function"), "foo") {
		t.Fatalf("functions=%v", symNames(r, "function"))
	}
	if !has(edgeRaws(r, "includes"), "./lib.sh") {
		t.Fatalf("includes=%v", edgeRaws(r, "includes"))
	}
	if !has(edgeRaws(r, "execs"), "~/.config/x/y.sh") {
		t.Fatalf("execs=%v", edgeRaws(r, "execs"))
	}
}

func TestPythonExtractor(t *testing.T) {
	r := mustExtract(t, "python", "import os\nfrom a.b import c\ndef run(x):\n    return x\nclass Bar:\n    pass\n")
	if !has(symNames(r, "function"), "run") || !has(symNames(r, "class"), "Bar") {
		t.Fatalf("syms f=%v c=%v", symNames(r, "function"), symNames(r, "class"))
	}
	inc := edgeRaws(r, "includes")
	if !has(inc, "os") || !has(inc, "a.b") {
		t.Fatalf("includes=%v", inc)
	}
}

func TestCSSExtractor(t *testing.T) {
	r := mustExtract(t, "css", ":root{--accent:#1e1e2e}\na{color:var(--accent);background:#fff}\n@import \"colors.css\";\n")
	if !has(edgeRaws(r, "declares_resource"), "--accent") {
		t.Fatalf("declares=%v", edgeRaws(r, "declares_resource"))
	}
	if !has(edgeRaws(r, "uses_resource"), "--accent") {
		t.Fatalf("uses=%v", edgeRaws(r, "uses_resource"))
	}
	if !has(edgeRaws(r, "includes"), "colors.css") {
		t.Fatalf("includes=%v", edgeRaws(r, "includes"))
	}
}

func TestHyprlangExtractor(t *testing.T) {
	src := "$mod = SUPER\ngeneral {\n  gaps_in = 5\n  col.active = rgb(1e1e2e)\n}\nsource = ~/.config/hypr/colors.conf\nbind = $mod, Q, exec, kitty\nexec-once = waybar\n"
	r := mustExtract(t, "hyprlang", src)
	if !has(symNames(r, "config_section"), "general") {
		t.Fatalf("sections=%v", symNames(r, "config_section"))
	}
	if !has(symNames(r, "setting"), "gaps_in") {
		t.Fatalf("settings=%v", symNames(r, "setting"))
	}
	if !has(edgeRaws(r, "includes"), "~/.config/hypr/colors.conf") {
		t.Fatalf("includes=%v", edgeRaws(r, "includes"))
	}
	if !has(symNames(r, "keybind"), "$mod Q") {
		t.Fatalf("keybinds=%v", symNames(r, "keybind"))
	}
	if !has(edgeRaws(r, "binds"), "kitty") {
		t.Fatalf("binds=%v", edgeRaws(r, "binds"))
	}
	if !has(edgeRaws(r, "execs"), "waybar") {
		t.Fatalf("execs=%v", edgeRaws(r, "execs"))
	}
	if !has(edgeRaws(r, "declares_resource"), "$mod") {
		t.Fatalf("declares=%v", edgeRaws(r, "declares_resource"))
	}
}

func TestQMLExtractor(t *testing.T) {
	r := mustExtract(t, "qml", "import QtQuick 2.0\nItem {\n  id: root\n  property int x: 1\n  Rectangle { id: rect }\n}\n")
	inst := edgeRaws(r, "instantiates")
	if !has(inst, "Item") || !has(inst, "Rectangle") {
		t.Fatalf("instantiates=%v", inst)
	}
	ids := symNames(r, "qml_id")
	if !has(ids, "root") || !has(ids, "rect") {
		t.Fatalf("ids=%v", ids)
	}
	if !has(edgeRaws(r, "includes"), "QtQuick") {
		t.Fatalf("includes=%v", edgeRaws(r, "includes"))
	}
}

func TestMarkdownExtractor(t *testing.T) {
	r := mustExtract(t, "markdown", "# Title\n\nbody text\n\n## Install steps\n\nSetext Head\n===========\n")
	h := symNames(r, "heading")
	if !has(h, "Title") || !has(h, "Install steps") || !has(h, "Setext Head") {
		t.Fatalf("headings=%v", h)
	}
	if len(r.Chunks) == 0 {
		t.Fatalf("expected chunks for full-text search")
	}
}

func TestJavaScriptExtractor(t *testing.T) {
	src := ".pragma library\nimport {a} from \"./mod.js\";\nconst lib = require(\"./other.js\");\nexport function build(x) { return x; }\nexport const make = (y) => y;\nclass Widget {\n  render() { return 1; }\n}\nvar data = { k: 1 };\nfunction helper(o) {\n  var local = 1;\n  return local;\n}\n"
	r := mustExtract(t, "javascript", src)
	fns := symNames(r, "function")
	if !has(fns, "build") || !has(fns, "make") || !has(fns, "helper") {
		t.Fatalf("funcs=%v", fns)
	}
	if !has(symNames(r, "class"), "Widget") {
		t.Fatalf("classes=%v", symNames(r, "class"))
	}
	if !has(symNames(r, "method"), "render") {
		t.Fatalf("methods=%v", symNames(r, "method"))
	}
	vars := symNames(r, "variable")
	if !has(vars, "data") {
		t.Fatalf("vars=%v", vars)
	}
	if has(vars, "local") {
		t.Fatalf("local inside a function body should not be a symbol: %v", vars)
	}
	inc := edgeRaws(r, "includes")
	if !has(inc, "./mod.js") || !has(inc, "./other.js") {
		t.Fatalf("includes=%v", inc)
	}
}
