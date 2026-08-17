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
