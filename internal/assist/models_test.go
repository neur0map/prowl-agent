package assist

import "testing"

func TestMatchModel(t *testing.T) {
	have := []string{"gemma3:4b", "nomic-embed-text:latest", "qwen3-embedding:4b"}
	cases := map[string]bool{
		"gemma3:4b":          true,
		"nomic-embed-text":   true, // bare name matches the implicit :latest
		"qwen3-embedding:4b": true,
		"qwen3-embedding:8b": false,
		"gemma3:1b":          false,
		"missing":            false,
	}
	for want, exp := range cases {
		if got := matchModel(have, want); got != exp {
			t.Errorf("matchModel(%q) = %v, want %v", want, got, exp)
		}
	}
}
