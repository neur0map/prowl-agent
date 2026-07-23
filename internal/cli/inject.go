package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const agentsMarker = "<!-- prowl-agent -->"

// agentsEndMarker closes the prowl-agent block in AGENTS.md.
const agentsEndMarker = "<!-- /prowl-agent -->"

const agentsBlock = agentsMarker + `
## Prowl project context

Use the local Prowl index before broad file reads. Results cite file and line ranges;
the index refreshes automatically.

- Start: ` + "`prowl-agent overview`" + `
- Locate code or knowledge: ` + "`prowl-agent find <name>`" + ` and ` + "`prowl-agent search <text>`" + `
- Understand dependencies: ` + "`prowl-agent callers|callees|relations <path>`" + `
- Before editing: ` + "`prowl-agent impact <path>`" + ` and ` + "`prowl-agent references <symbol_id>`" + `
- After editing: ` + "`prowl-agent changed`" + ` and ` + "`prowl-agent doctor`" + `
- Dotfile/desktop checks: ` + "`prowl-agent doctor --profile rice`" + `

Use ` + "`--format human|toon|json|markdown`" + ` as needed; non-terminal output
remains token-lean TOON by default. MCP clients can launch ` + "`prowl-agent serve`" + `.
<!-- /prowl-agent -->`

// Inject writes MCP server configs for common agent environments and agent
// instructions (AGENTS.md). It covers the standard `.mcp.json` (most agents),
// Cursor, VS Code, Oh My Pi (`.omp/mcp.json`), Factory droid
// (`.factory/mcp.json`), and OpenCode (`opencode.json`, a distinct shape). Every
// write merges into existing config and is idempotent.
func Inject(root string) error {
	for _, c := range []struct{ path, key string }{
		{filepath.Join(root, ".mcp.json"), "mcpServers"},            // standard / generic
		{filepath.Join(root, ".cursor", "mcp.json"), "mcpServers"},  // Cursor
		{filepath.Join(root, ".vscode", "mcp.json"), "servers"},     // VS Code
		{filepath.Join(root, ".omp", "mcp.json"), "mcpServers"},     // Oh My Pi
		{filepath.Join(root, ".factory", "mcp.json"), "mcpServers"}, // Factory droid
	} {
		if err := mergeMCPConfig(c.path, c.key); err != nil {
			return err
		}
	}
	if err := mergeOpenCode(filepath.Join(root, "opencode.json")); err != nil {
		return err
	}
	return ensureAgentsBlock(filepath.Join(root, "AGENTS.md"))
}

type mcpServer struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// mergeMCPConfig adds the prowl-agent server under the given top-level key
// (mcpServers shape for the generic and Cursor configs, servers for VS Code),
// preserving existing entries. Invalid existing JSON is refused rather than
// replaced; parent directories are created when needed.
func mergeMCPConfig(path, key string) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	doc := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("refusing to replace invalid JSON in %s: %w", path, err)
		}
	}
	servers, _ := doc[key].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["prowl-agent"] = mcpServer{Type: "stdio", Command: "prowl-agent", Args: []string{"serve"}}
	doc[key] = servers
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// mergeOpenCode adds the prowl-agent server to an OpenCode config (opencode.json),
// which uses a distinct shape: an `mcp` map of local servers with a command array.
// Existing keys are preserved.
func mergeOpenCode(path string) error {
	doc := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("refusing to replace invalid JSON in %s: %w", path, err)
		}
	}
	if _, ok := doc["$schema"]; !ok {
		doc["$schema"] = "https://opencode.ai/config.json"
	}
	mcp, _ := doc["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	mcp["prowl-agent"] = map[string]any{
		"type":    "local",
		"command": []string{"prowl-agent", "serve"},
		"enabled": true,
	}
	doc["mcp"] = mcp
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

func ensureAgentsBlock(path string) error {
	data, _ := os.ReadFile(path)
	content := string(data)
	// Refresh an existing prowl block in place, so a re-init (after a reboot, or a
	// prowl-agent upgrade) replaces stale guidance instead of leaving it. Only the
	// span between our markers is touched, so anything outside them (the user's
	// own AGENTS.md) is preserved. A well-formed block has both markers; if only
	// the opening marker survives (a hand-edited or truncated file) we replace
	// just that marker line, never the rest of the file, so user text below it is
	// never deleted.
	if i := strings.Index(content, agentsMarker); i >= 0 {
		var end int
		switch {
		case strings.Contains(content[i:], agentsEndMarker):
			end = i + strings.Index(content[i:], agentsEndMarker) + len(agentsEndMarker)
		case strings.IndexByte(content[i:], '\n') >= 0:
			end = i + strings.IndexByte(content[i:], '\n') // no closing marker: only the opening line
		default:
			end = len(content) // opening marker is the final line, nothing follows
		}
		updated := content[:i] + agentsBlock + content[end:]
		if updated == content {
			return nil
		}
		return os.WriteFile(path, []byte(updated), 0o644)
	}
	var b strings.Builder
	b.WriteString(content)
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		b.WriteByte('\n')
	}
	if len(content) > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(agentsBlock + "\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
