package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const agentsMarker = "<!-- prowl-agent -->"

const agentsBlock = agentsMarker + `
## Prowl Agent (code intelligence)

This rice is indexed by **prowl-agent** (MCP server: ` + "`prowl-agent serve`" + `).
**Prefer prowl-agent queries before reading files manually.** Use them to narrow
the search space, then open only the files they point to:

- ` + "`find_symbol`, `find_references`, `find_callers`, `find_callees`" + `
- ` + "`file_relations`, `blast_radius`, `entrypoints_for`, `tests_for`" + `
- ` + "`similar_code`, `architecture_violations`, `repo_hotspots`, `status`" + `
<!-- /prowl-agent -->`

// Inject writes MCP server configs for common agent environments: a generic
// `.mcp.json`, Cursor (`.cursor/mcp.json`), and VS Code (`.vscode/mcp.json`),
// plus agent instructions (`AGENTS.md`). Every write merges and is idempotent.
func Inject(root string) error {
	if err := mergeMCPConfig(filepath.Join(root, ".mcp.json"), "mcpServers"); err != nil {
		return err
	}
	if err := mergeMCPConfig(filepath.Join(root, ".cursor", "mcp.json"), "mcpServers"); err != nil {
		return err
	}
	if err := mergeMCPConfig(filepath.Join(root, ".vscode", "mcp.json"), "servers"); err != nil {
		return err
	}
	return ensureAgentsBlock(filepath.Join(root, "AGENTS.md"))
}

type mcpServer struct {
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
	servers["prowl-agent"] = mcpServer{Command: "prowl-agent", Args: []string{"serve"}}
	doc[key] = servers
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

func ensureAgentsBlock(path string) error {
	data, _ := os.ReadFile(path)
	content := string(data)
	if strings.Contains(content, agentsMarker) {
		return nil
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
