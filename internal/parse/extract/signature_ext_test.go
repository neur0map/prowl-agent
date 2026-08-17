package extract

import (
	"strings"
	"testing"
)

// sigOf returns the recorded signature of the first symbol named want.
func sigOf(r Result, want string) string {
	for _, s := range r.Symbols {
		if s.Name == want {
			return s.Signature
		}
	}
	return ""
}

// TestSignaturesPopulated verifies every code extractor records a declaration
// signature, so `find` shows a symbol's interface without opening the file. Each
// case asserts the signature carries the parameters/return without leaking the
// body brace.
func TestSignaturesPopulated(t *testing.T) {
	cases := []struct {
		lang, src, sym string
		want           []string // substrings that must appear
	}{
		{"go", "package p\nfunc Add(a int, b int) (int, error) { return a + b, nil }\n", "Add",
			[]string{"func Add(a int, b int) (int, error)"}},
		{"python", "def greet(name: str) -> str:\n    return name\n", "greet",
			[]string{"def greet(name: str) -> str:"}},
		{"rust", "pub fn add(a: i32, b: i32) -> i32 { a + b }\n", "add",
			[]string{"fn add(a: i32, b: i32) -> i32"}},
		{"ruby", "def greet(name)\n  name\nend\n", "greet",
			[]string{"def greet(name)"}},
		{"java", "class C {\n  public int add(int a, int b) { return a + b; }\n}\n", "add",
			[]string{"public int add(int a, int b)"}},
		{"typescript", "export function add(a: number, b: number): number { return a + b; }\n", "add",
			[]string{"function add(a: number, b: number): number"}},
	}
	for _, c := range cases {
		r := mustExtract(t, c.lang, c.src)
		got := sigOf(r, c.sym)
		if got == "" {
			t.Errorf("%s: no signature recorded for %q", c.lang, c.sym)
			continue
		}
		if strings.Contains(got, "{") {
			t.Errorf("%s: signature for %q leaked the body brace: %q", c.lang, c.sym, got)
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: signature for %q = %q, want substring %q", c.lang, c.sym, got, w)
			}
		}
	}
}

func TestClipSignatureCaps(t *testing.T) {
	long := "func F(" + strings.Repeat("x int, ", 80) + ")"
	got := clipSignature(long)
	if len(got) > 210 {
		t.Errorf("clipSignature did not cap length: %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("capped signature should end with ellipsis: %q", got)
	}
	// Whitespace is collapsed to single spaces.
	if clipSignature("func  F(\n\ta int)") != "func F( a int)" {
		t.Errorf("whitespace not collapsed: %q", clipSignature("func  F(\n\ta int)"))
	}
}

// A Go struct or interface has no `body` field to stop the header at, so its
// whole body is flattened into the signature; inline field comments must not
// survive, or once newlines collapse to spaces a `// ...` runs into the next
// field (the outline corruption `InferencerProvider // VectorProgress...`).
func TestSignatureElidesStructAndInterfaceComments(t *testing.T) {
	cases := []struct{ lang, src, sym string }{
		{"go", "package p\ntype Options struct {\n\tName string // short name\n\tProvider Inferencer // when set, receives progress\n}\n", "Options"},
		{"go", "package p\ntype Doer interface {\n\tDo() error // does the thing\n}\n", "Doer"},
	}
	for _, c := range cases {
		got := sigOf(mustExtract(t, c.lang, c.src), c.sym)
		if got == "" {
			t.Errorf("%s: no signature for %q", c.lang, c.sym)
			continue
		}
		if strings.Contains(got, "//") || strings.Contains(got, "/*") {
			t.Errorf("%s: signature for %q leaked a comment: %q", c.lang, c.sym, got)
		}
		// The field declarations themselves are kept (option-a fix is informative).
		if c.sym == "Options" && !strings.Contains(got, "Provider Inferencer") {
			t.Errorf("field declaration dropped along with the comment: %q", got)
		}
	}
}
