package embed

import (
	"math"
	"testing"
)

func almost(got, want []float32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if math.Abs(float64(got[i]-want[i])) > 1e-5 {
			return false
		}
	}
	return true
}

// TestEmbedMeanPoolNormalized checks the pooling math on a tiny synthetic model,
// with no network or cached weights needed.
func TestEmbedMeanPoolNormalized(t *testing.T) {
	m := &Model{
		vocab:    map[string]int{"[UNK]": 0, "a": 1, "b": 2},
		matrix:   []float32{0, 0, 3, 0, 0, 4}, // rows: [UNK]=(0,0) a=(3,0) b=(0,4)
		rows:     3,
		dim:      2,
		unkID:    0,
		maxChars: 100,
	}
	if got := m.embedOne("a a"); !almost(got, []float32{1, 0}) { // mean (3,0) -> unit (1,0)
		t.Fatalf("embed(\"a a\") = %v, want [1 0]", got)
	}
	if got := m.embedOne("a b"); !almost(got, []float32{0.6, 0.8}) { // mean (1.5,2) -> unit (0.6,0.8)
		t.Fatalf("embed(\"a b\") = %v, want [0.6 0.8]", got)
	}
	if got := m.embedOne("zzzzz"); got[0] != 0 || got[1] != 0 { // unknown -> zero row -> zero vec
		t.Fatalf("embed(unknown) = %v, want [0 0]", got)
	}
}

// TestEmbedWeightedMean checks weighted mean-pooling: a larger per-token weight
// pulls the result toward that token's vector (the potion-code model ships such
// weights).
func TestEmbedWeightedMean(t *testing.T) {
	m := &Model{
		vocab:    map[string]int{"[UNK]": 0, "a": 1, "b": 2},
		matrix:   []float32{0, 0, 1, 0, 0, 1}, // [UNK]=(0,0) a=(1,0) b=(0,1)
		weights:  []float64{1, 3, 1},          // token "a" weighted 3x
		rows:     3,
		dim:      2,
		unkID:    0,
		maxChars: 100,
	}
	// weighted mean = (3*(1,0)+1*(0,1))/4 = (0.75,0.25) -> unit (0.9487,0.3162)
	if got := m.embedOne("a b"); !almost(got, []float32{0.9486833, 0.3162278}) {
		t.Fatalf("weighted embed(\"a b\") = %v, want ~[0.9487 0.3162]", got)
	}
}

// TestStaticEmbedSemantic verifies the bundled model ranks a semantically related
// phrase above an unrelated one, even with almost no shared tokens.
func TestStaticEmbedSemantic(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatalf("bundled model failed to load: %v", err)
	}
	cos := func(a, b string) float64 {
		va, vb := m.embedOne(a), m.embedOne(b)
		var d float64
		for i := range va {
			d += float64(va[i]) * float64(vb[i])
		}
		return d
	}
	related := cos("refresh the ai usage widget", "how do I update the AI usage indicator")
	unrelated := cos("refresh the ai usage widget", "parse a yaml configuration file")
	if related <= unrelated {
		t.Fatalf("semantic ordering broken: related=%.3f unrelated=%.3f", related, unrelated)
	}
	if related < 0.3 {
		t.Fatalf("related similarity too low: %.3f", related)
	}
}
