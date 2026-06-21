package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const agentsMarker = "<!-- prowl-agent -->"

// agentsEndMarker closes the prowl-agent block in AGENTS.md.
const agentsEndMarker = "<!-- /prowl-agent -->"

const agentsBlock = agentsMarker + `
## Prowl Agent (code intelligence)

This project is indexed by **prowl-agent**. Query the index from your shell
instead of grepping and reading whole files. Answers are cited (file:line),
ranked, and token-lean (TOON format, ~40% smaller than JSON, and read more
accurately by models). The index refreshes itself on each call, so there is
nothing to start and nothing goes stale.

**Prefer a prowl-agent query before reading files manually.** Open a raw file
only after a query points you to the exact lines.

### Commands

    prowl-agent overview            # project map: docs to read, roles, entrypoints, clusters (start here)
    prowl-agent find <name>         # locate a symbol (function, setting, keybind, component)
    prowl-agent search <text>       # search content; add --smart (rerank) or --compact (files only)
    prowl-agent callers <path>      # what includes / execs / binds to a file
    prowl-agent callees <path>      # what a file includes / execs / binds to
    prowl-agent impact <path>       # blast radius: dependent count, subsystems, direct importers (--all = full list)
    prowl-agent relations <path>    # a file's symbols and include neighbors
    prowl-agent entrypoints <path>  # root files from which this file is reachable
    prowl-agent references <id>     # where a symbol is used: ref edges, or call sites for code (id from 'find')
    prowl-agent clusters            # subsystems (connected files)
    prowl-agent hotspots            # structurally central / large files
    prowl-agent violations          # dangling refs, orphan scripts, hardcoded colors
    prowl-agent doctor              # health: cycles, duplicate keybinds, broken commands
    prowl-agent tests <path>        # configs/keybinds that launch or reload a file
    prowl-agent changed             # your git changes mapped to the files they could affect

Every command accepts --json for JSON instead of TOON, and --limit N to cap
results (fewer tokens). Run from anywhere inside the project; prowl-agent finds
the index by walking up to .prowl/.

### When to use which

- New or unfamiliar project: overview, then clusters to pull a whole subsystem.
- After a find: results carry line and end_line, so read just that range, not the whole file.
- Fuzzy / natural-language question: search "<text>" (add --smart); --compact lists files first.
- Before changing a color/font/variable: find it, then references <id> for every usage; check violations.
- Before editing or deleting a file: impact <path> for what breaks, callers <path> for what invokes it.
- Adding a keybind: doctor first, to avoid duplicate-keybind clashes.
- Tracing startup: entrypoints <path> for the entry point and autostart chain.
- After editing, or before committing: changed to see what your edits could affect, then doctor.

The same index is also available over MCP (server: ` + "`prowl-agent serve`" + `) for
agents that prefer typed tools, but the shell commands above are the
recommended, lowest-overhead path (no server, no per-call schema cost).
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
// preserving existing entries. A bad existing file is replaced; parent dirs created.
func mergeMCPConfig(path, key string) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	doc := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &doc)
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
		_ = json.Unmarshal(data, &doc)
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
	// Refresh an existing block in place, so a re-init (after a reboot, or after a
	// prowl-agent upgrade) replaces stale guidance instead of leaving it. Anything
	// outside the markers (the user's own AGENTS.md) is preserved.
	if i := strings.Index(content, agentsMarker); i >= 0 {
		end := len(content)
		if j := strings.Index(content[i:], agentsEndMarker); j >= 0 {
			end = i + j + len(agentsEndMarker)
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
