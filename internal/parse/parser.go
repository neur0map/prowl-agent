package parse

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/alexaandru/go-sitter-forest/bash"
	"github.com/alexaandru/go-sitter-forest/cpp"
	"github.com/alexaandru/go-sitter-forest/css"
	"github.com/alexaandru/go-sitter-forest/fish"
	"github.com/alexaandru/go-sitter-forest/hyprlang"
	"github.com/alexaandru/go-sitter-forest/ini"
	"github.com/alexaandru/go-sitter-forest/javascript"
	"github.com/alexaandru/go-sitter-forest/json"
	"github.com/alexaandru/go-sitter-forest/lua"
	"github.com/alexaandru/go-sitter-forest/markdown"
	"github.com/alexaandru/go-sitter-forest/python"
	"github.com/alexaandru/go-sitter-forest/qmljs"
	"github.com/alexaandru/go-sitter-forest/scss"
	"github.com/alexaandru/go-sitter-forest/toml"
	"github.com/alexaandru/go-sitter-forest/yaml"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

// grammars maps a canonical language id to its Tree-sitter grammar constructor.
// Languages without an entry (rasi, generic) are handled by the generic,
// line-oriented extractor instead.
var grammars = map[string]func() unsafe.Pointer{
	"lua":        lua.GetLanguage,
	"python":     python.GetLanguage,
	"bash":       bash.GetLanguage,
	"css":        css.GetLanguage,
	"scss":       scss.GetLanguage,
	"json":       json.GetLanguage,
	"yaml":       yaml.GetLanguage,
	"toml":       toml.GetLanguage,
	"ini":        ini.GetLanguage,
	"hyprlang":   hyprlang.GetLanguage,
	"qml":        qmljs.GetLanguage,
	"cpp":        cpp.GetLanguage,
	"fish":       fish.GetLanguage,
	"javascript": javascript.GetLanguage,
	"markdown":   markdown.GetLanguage,
}

// HasGrammar reports whether lang has a Tree-sitter grammar.
func HasGrammar(lang string) bool { _, ok := grammars[lang]; return ok }

// Language returns the sitter.Language for lang, if one exists.
func Language(lang string) (*sitter.Language, bool) {
	g, ok := grammars[lang]
	if !ok {
		return nil, false
	}
	return sitter.NewLanguage(g()), true
}

// Parse parses src with the grammar for lang. The caller MUST call tree.Close().
func Parse(lang string, src []byte) (*sitter.Tree, error) {
	lng, ok := Language(lang)
	if !ok {
		return nil, fmt.Errorf("no grammar for %q", lang)
	}
	p := sitter.NewParser()
	if !p.SetLanguage(lng) {
		return nil, fmt.Errorf("set language %q", lang)
	}
	return p.ParseString(context.Background(), nil, src)
}
