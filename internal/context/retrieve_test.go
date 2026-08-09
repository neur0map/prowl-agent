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

// TestNamesToConcept pins the path-match gate: a basename must carry enough of the
// query's concept terms to count (two for multi-word queries, one for single),
// so cursorRenderer.ts/projectPersistence.ts match while an incidental single-term
// hit (rank.go for "...ranking") does not -- the flood that regressed the
// retrieval benchmark before the gate existed.
func TestNamesToConcept(t *testing.T) {
	cases := []struct {
		terms []string
		rel   string
		want  bool
	}{
		{[]string{"project", "persistence"}, "src/components/projectPersistence.ts", true},
		{[]string{"cursor", "renderer"}, "src/videoPlayback/cursorRenderer.ts", true},
		{[]string{"frame", "authority", "ranking"}, "internal/context/rank.go", false},
		{[]string{"project", "persistence"}, "src/persistence/helpers.ts", false}, // dir match only
		{[]string{"config"}, "src/appConfig.ts", true},                            // single-term query
		{[]string{"cache"}, "internal/store/graph.go", false},
	}
	for _, c := range cases {
		if got := namesToConcept(c.terms, c.rel); got != c.want {
			t.Errorf("namesToConcept(%v, %q) = %v, want %v", c.terms, c.rel, got, c.want)
		}
	}
}
