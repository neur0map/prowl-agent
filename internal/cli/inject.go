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

This project is indexed by **prowl-agent** (MCP server: ` + "`prowl-agent serve`" + `).
**Prefer prowl-agent queries before reading files manually.** They return cited,
bounded context; open raw files only after a query points you to them.

Tools: ` + "`overview`, `clusters`, `find_symbol`, `find_references`, `find_callers`, `find_callees`, `file_relations`, `blast_radius`, `entrypoints_for`, `tests_for`, `similar_code`, `smart_search`, `architecture_violations`, `repo_hotspots`, `doctor`, `status`" + `.

### How to use these tools

- **New session / unfamiliar project:** call ` + "`overview`" + ` first, then ` + "`clusters`" + ` to grab a whole subsystem.
- **Fuzzy / natural-language question:** use ` + "`smart_search`" + ` (or ` + "`similar_code`" + `); pass ` + "`detail: compact`" + ` to list files before pulling snippets.
- **Before changing a color/font/variable:** ` + "`find_symbol`" + ` it, then ` + "`find_references`" + ` to see every usage; check ` + "`architecture_violations`" + ` for hardcoded duplicates.
- **Before editing or deleting a file or script:** run ` + "`blast_radius`" + ` to see what breaks, and ` + "`find_callers`" + ` to see what invokes it.
- **Adding a keybind:** run ` + "`doctor`" + ` first to avoid ` + "`duplicate_keybind`" + ` conflicts.
- **Tracing startup:** ` + "`entrypoints_for`" + ` a file to find the entry point and autostart chain.
- **Before committing:** run ` + "`doctor`" + ` and resolve errors (cycles, dangling refs, broken commands).
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
