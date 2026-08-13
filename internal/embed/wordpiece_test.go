package embed

import (
	"reflect"
	"testing"
)

// tokModel loads only the tokenizer (no matrix) for tokenization tests, from the
// binary-bundled tokenizer bytes.
func tokModel(t *testing.T) *Model {
	t.Helper()
	vocab, unk, maxChars, err := loadTokenizer(bundledTokenizer)
	if err != nil {
		t.Fatal(err)
	}
	return &Model{vocab: vocab, unkID: unk, maxChars: maxChars}
}

// TestWordPieceMatchesReference pins the Go tokenizer to the exact token IDs the
// HuggingFace tokenizer produces (add_special_tokens=False) for the potion
// model. Since the embedding is a mean of these tokens' rows in the identical
// safetensors matrix, matching IDs means matching vectors -- this is the load-
// bearing fidelity check for the whole embedder.
func TestWordPieceMatchesReference(t *testing.T) {
	m := tokModel(t)
	cases := []struct {
		text string
		ids  []int
	}{
		{"refresh the ai usage widget", []int{29711, 999, 8935, 7195, 29728}},
		{"How do I refresh the widget?", []int{1132, 1082, 48, 29711, 999, 29728, 32}},
		{"banana", []int{14215}},
		{"the quick brown fox", []int{999, 3251, 1832, 3422}},
		{"RefreshWidget()  // re-reads cache", []int{29711, 8151, 23294, 9, 10, 16, 16, 1131, 14, 8634, 16056}},
		{"hello, world!", []int{6595, 13, 1091, 2}},
		{"CamelCaseIdentifier", []int{33245, 4181, 15781, 7876, 1124}},
		{"snake_case_name", []int{6491, 38, 1556, 38, 1174}},
		{"don't  über  café", []int{1126, 8, 59, 18172, 6671}},
	}
	for _, c := range cases {
		got := m.tokenizeIDs(c.text)
		if !reflect.DeepEqual(got, c.ids) {
			t.Errorf("tokenize(%q)\n  got  %v\n  want %v", c.text, got, c.ids)
		}
	}
}

// TestWordPieceUnknown covers the unsegmentable-word path.
func TestWordPieceUnknown(t *testing.T) {
	m := tokModel(t)
	// A run of a character with no vocab entry collapses to the unknown token.
	got := m.tokenizeIDs("\u0800\u0800\u0800")
	if len(got) != 1 || got[0] != m.unkID {
		t.Fatalf("unknown word = %v, want [%d]", got, m.unkID)
	}
}
