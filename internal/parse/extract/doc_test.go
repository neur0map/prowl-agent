package extract

import "testing"

func TestDocSentenceStripsMarkersAndStopsAtFirstSentence(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"go line comments",
			"// ensureFresh brings the derived index up to date before any query is\n// served. A stale structural index is reindexed.",
			"ensureFresh brings the derived index up to date before any query is served."},
		{"python docstring",
			"\"\"\"Regex-based secret redaction for logs and tool output.\n\nApplies pattern matching to mask API keys.\n\"\"\"",
			"Regex-based secret redaction for logs and tool output."},
		{"block comment",
			"/* Warm loads model into memory. Best-effort. */",
			"Warm loads model into memory."},
		{"no terminator", "// helper for the thing", "helper for the thing"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DocSentence(c.in); got != c.want {
				t.Fatalf("DocSentence()\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestDocCommentStartWalksContiguousComments(t *testing.T) {
	lines := []string{"package p", "", "// one", "// two", "func F() {}"}
	if got := DocCommentStart(lines, 5); got != 3 {
		t.Fatalf("DocCommentStart = %d, want 3", got)
	}
	// A blank line breaks the block: the comment above it is not this symbol's doc.
	lines = []string{"// stray", "", "func F() {}"}
	if got := DocCommentStart(lines, 3); got != 3 {
		t.Fatalf("DocCommentStart = %d, want 3 (blank line breaks the block)", got)
	}
}

// A Python contract lives in the docstring that opens a def/class body, not in a
// leading comment, so the upward comment walk never sees it. The extractor must
// capture it into Symbol.Doc so this high-signal prose can be scored on its own.
func TestPythonDocstringCaptured(t *testing.T) {
	src := "class Redactor:\n" +
		"    \"\"\"Log formatter that redacts secrets from all log messages.\"\"\"\n" +
		"    def mask(self, s):\n" +
		"        \"\"\"Mask any secret before it reaches the log file.\"\"\"\n" +
		"        return s\n" +
		"def plain():\n" +
		"    return 1\n"
	r := mustExtract(t, "python", src)
	docOf := func(name string) string {
		for _, s := range r.Symbols {
			if s.Name == name {
				return s.Doc
			}
		}
		return "<no symbol>"
	}
	if got := docOf("Redactor"); got != "Log formatter that redacts secrets from all log messages." {
		t.Errorf("class doc = %q", got)
	}
	if got := docOf("mask"); got != "Mask any secret before it reaches the log file." {
		t.Errorf("method doc = %q", got)
	}
	if got := docOf("plain"); got != "" {
		t.Errorf("function without a docstring should have empty Doc, got %q", got)
	}
}
