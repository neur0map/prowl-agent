package parse

import "testing"

func TestParseAllGrammars(t *testing.T) {
	cases := map[string]string{
		"lua":      "local x = 1",
		"python":   "x = 1",
		"bash":     "x=1",
		"css":      "a{color:red}",
		"scss":     "$x: red;",
		"json":     "{}",
		"yaml":     "a: 1",
		"toml":     "a = 1",
		"ini":      "[s]\na=1",
		"hyprlang": "$x = 1",
		"qml":      "import QtQuick\nItem { }",
		"cpp":      "int main() { return 0; }",
		"fish":     "function f\nend",
	}
	for lang, src := range cases {
		tree, err := Parse(lang, []byte(src))
		if err != nil {
			t.Errorf("Parse(%s): %v", lang, err)
			continue
		}
		if root := tree.RootNode(); root.Type() == "" {
			t.Errorf("Parse(%s): empty root type", lang)
		}
		tree.Close()
	}
	if _, err := Parse("nope", []byte("x")); err == nil {
		t.Error("Parse(nope) should error")
	}
}
