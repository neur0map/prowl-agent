package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/config"
)

// In non-interactive mode setupAI prints the chosen tier and never runs an
// installer or a pull, so it is safe to exercise without Ollama present.
func TestSetupAINonInteractive(t *testing.T) {
	var b strings.Builder
	setupAI(context.Background(), &b, config.PresetByName("fast"), false)
	s := b.String()
	if !strings.Contains(s, "fast") || !strings.Contains(s, "embeddinggemma") {
		t.Fatalf("setupAI output missing tier/model:\n%s", s)
	}
}
