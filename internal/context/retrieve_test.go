package context

import (
	"slices"
	"testing"
)

// TestTermForms pins the light stemmer behind the symbol-authority signal: an
// inflected query word must yield a base-form stem so it still substring-matches
// a base-form symbol name (the real-repo miss that "parsed"/"indexing"/"files"
// failed to match parseFile/indexWithOptions before this existed).
func TestTermForms(t *testing.T) {
	cases := []struct {
		term string
		want string // a stem that must be present
	}{
		{"indexing", "index"},
		{"parsed", "pars"},
		{"files", "file"},
		{"caches", "cache"},
		{"parse", "parse"}, // no suffix: base form retained
	}
	for _, c := range cases {
		forms := termForms(c.term)
		if !slices.Contains(forms, c.want) {
			t.Errorf("termForms(%q) = %v, want to contain %q", c.term, forms, c.want)
		}
		if !slices.Contains(forms, c.term) {
			t.Errorf("termForms(%q) = %v, must retain the original term", c.term, forms)
		}
	}
}
