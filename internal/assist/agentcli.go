package assist

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// AgentCLI is an Inferencer that borrows an installed coding-agent CLI
// ("claude -p", "codex exec", "omp -p", ...) as its model, so semantic
// reranking works with no local model daemon and from the CLI -- not just over
// MCP sampling. It runs the agent once in non-interactive completion mode and
// reads the answer from stdout.
//
// It implements reranking and free-form generation, the parts that most improve
// search quality. Coding-agent CLIs do not expose embeddings, so Embed returns
// an error and vector search stays off; structural search is unaffected.
type AgentCLI struct {
	// Argv is the completion invocation: the binary plus the flags that put it
	// in non-interactive mode. The prompt is appended as the final argument.
	Argv    []string
	Timeout time.Duration
}

var _ Inferencer = (*AgentCLI)(nil)

// NewAgentCLI parses a command string like "claude -p" or "omp -p" into an
// AgentCLI. The first field is the binary; the rest are fixed flags.
func NewAgentCLI(command string) *AgentCLI {
	return &AgentCLI{Argv: strings.Fields(command), Timeout: 45 * time.Second}
}

// Command reports the configured invocation for user-facing messages.
func (a *AgentCLI) Command() string { return strings.Join(a.Argv, " ") }

// Available reports whether the agent binary is resolvable on PATH.
func (a *AgentCLI) Available(_ context.Context) bool {
	if len(a.Argv) == 0 {
		return false
	}
	_, err := exec.LookPath(a.Argv[0])
	return err == nil
}

// Generate runs the agent once with the prompt appended as the final argument
// (no shell, so nothing in the prompt is interpreted) and returns its stdout,
// bounded by Timeout.
func (a *AgentCLI) Generate(ctx context.Context, prompt string) (string, error) {
	if len(a.Argv) == 0 {
		return "", fmt.Errorf("agent CLI command not configured")
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := append(append([]string(nil), a.Argv[1:]...), prompt)
	cmd := exec.CommandContext(ctx, a.Argv[0], args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("agent %q: %s", a.Argv[0], msg)
	}
	return stdout.String(), nil
}

// Rerank orders docs by relevance using the agent as the model, sharing the
// constrained ordering prompt and tolerant parser with the local inferencer.
func (a *AgentCLI) Rerank(ctx context.Context, query string, docs []string) ([]int, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	resp, err := a.Generate(ctx, rerankPrompt(query, docs))
	if err != nil {
		return nil, err
	}
	return parseOrder(resp, len(docs)), nil
}

// Embed is unsupported: coding-agent CLIs do not expose embeddings. Callers
// treat the error as "no vector search"; reranking and structural search work.
func (a *AgentCLI) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, fmt.Errorf("agent CLI backend does not support embeddings")
}

// SupportsEmbeddings reports false: this backend reranks and generates but has
// no embedding endpoint, so the refresh skips vector building.
func (a *AgentCLI) SupportsEmbeddings() bool { return false }
