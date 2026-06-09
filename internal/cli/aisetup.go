package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"charm.land/huh/v2"

	"github.com/prowl-agent/prowl-agent/internal/assist"
	"github.com/prowl-agent/prowl-agent/internal/config"
)

// selectTier asks the user to choose an AI model tier.
func selectTier() string {
	tier := config.DefaultTier
	opts := make([]huh.Option[string], 0, len(config.Presets))
	for _, p := range config.Presets {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%-6s %s", p.Name, p.Desc), p.Name))
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Pick an AI model tier").
			Description("Bigger tiers retrieve better but need more VRAM and disk.").
			Options(opts...).
			Value(&tier),
	))
	if form.Run() != nil {
		return config.DefaultTier
	}
	return tier
}

// setupAI gets Ollama and the chosen tier's models ready. When interactive it
// offers to run the official installer and to pull missing models; otherwise it
// prints the commands to run.
func setupAI(ctx context.Context, out io.Writer, p config.ModelPreset, interactive bool) {
	fmt.Fprintf(out, "AI tier %q: embed %s, assist %s\n", p.Name, p.EmbedModel, p.AssistModel)
	oll := assist.NewOllama("", p.EmbedModel, p.AssistModel)

	if !oll.Available(ctx) {
		if _, lookErr := exec.LookPath("ollama"); lookErr != nil {
			if interactive && confirmAI("Ollama is not installed. Install it now? (runs the official installer; may ask for sudo)") {
				installOllama(out)
				waitForOllama(ctx, oll)
			} else {
				fmt.Fprintln(out, "  Install Ollama: curl -fsSL https://ollama.com/install.sh | sh")
				fmt.Fprintf(out, "  Then pull: ollama pull %s && ollama pull %s\n", p.EmbedModel, p.AssistModel)
				return
			}
		}
	}
	if !oll.Available(ctx) {
		fmt.Fprintln(out, "  Ollama is installed but not reachable. Start it, then pull the models:")
		fmt.Fprintf(out, "    ollama serve &\n    ollama pull %s\n    ollama pull %s\n", p.EmbedModel, p.AssistModel)
		return
	}

	for _, m := range []string{p.EmbedModel, p.AssistModel} {
		if oll.HasModel(ctx, m) {
			fmt.Fprintf(out, "  %s already installed\n", m)
			continue
		}
		if interactive && confirmAI(fmt.Sprintf("Pull %s now?", m)) {
			if err := pullModel(m); err != nil {
				fmt.Fprintf(out, "  pull failed: %v (run: ollama pull %s)\n", err, m)
			}
		} else {
			fmt.Fprintf(out, "  Pull it: ollama pull %s\n", m)
		}
	}
	fmt.Fprintln(out, "  Semantic search activates on 'prowl-agent serve'.")
}

func confirmAI(title string) bool {
	var ok bool
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Affirmative("Yes").Negative("No").Value(&ok),
	))
	if form.Run() != nil {
		return false
	}
	return ok
}

func installOllama(out io.Writer) {
	fmt.Fprintln(out, "Installing Ollama ...")
	cmd := exec.Command("sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(out, "  installer exited with: %v\n", err)
	}
}

func pullModel(model string) error {
	cmd := exec.Command("ollama", "pull", model)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// waitForOllama gives a just-installed daemon a few seconds to come up.
func waitForOllama(ctx context.Context, oll *assist.Ollama) {
	for i := 0; i < 10; i++ {
		if oll.Available(ctx) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
