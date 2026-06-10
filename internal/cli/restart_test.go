package cli

import "testing"

func TestMatchProwlServer(t *testing.T) {
	bin := "/home/u/.local/bin/prowl-agent"
	cases := []struct {
		name  string
		args  []string
		cwd   string
		scope string
		want  bool
	}{
		{"all-scope serve", []string{bin, "serve"}, "/any", "", true},
		{"all-scope lsp", []string{bin, "lsp"}, "/any", "", true},
		{"init is not a server", []string{bin, "init"}, "/any", "", false},
		{"non-prowl binary", []string{"/usr/bin/serve", "serve"}, "/any", "", false},
		{"too few args", []string{bin}, "/any", "", false},
		{"scoped match in root", []string{bin, "serve"}, "/proj/a", "/proj/a", true},
		{"scoped match nested", []string{bin, "lsp"}, "/proj/a/sub", "/proj/a", true},
		{"scoped no match outside", []string{bin, "serve"}, "/proj/b", "/proj/a", false},
		{"scoped no match prefix trick", []string{bin, "serve"}, "/proj/ab", "/proj/a", false},
	}
	for _, c := range cases {
		if got := matchProwlServer(c.args, c.cwd, c.scope); got != c.want {
			t.Errorf("%s: matchProwlServer(%v, %q, %q) = %v, want %v", c.name, c.args, c.cwd, c.scope, got, c.want)
		}
	}
}
