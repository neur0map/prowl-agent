package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/assist"
	"github.com/prowl-agent/prowl-agent/internal/config"
)

// In non-interactive mode setupAI reports the chosen tier and never runs an
// installer or a pull. The daemon-management hook is stubbed so the test never
// touches systemd or spawns ollama.
func TestSetupAINonInteractive(t *testing.T) {
	orig := ensureOllama
	ensureOllama = func(context.Context, *assist.Ollama, string) bool { return false }
	defer func() { ensureOllama = orig }()

	var b strings.Builder
	setupAI(context.Background(), &b, config.PresetByName("fast"), false)
	s := b.String()
	if !strings.Contains(s, "fast") || !strings.Contains(s, "embeddinggemma") {
		t.Fatalf("setupAI output missing tier/model:\n%s", s)
	}
}
