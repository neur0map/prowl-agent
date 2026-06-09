package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

// helixLangs are the config formats prowl-agent attaches to in Helix. Helix
// replaces the per-language server list, so we keep this to formats that rarely
// have a competing language server in a dotfiles/config project.
var helixLangs = []string{"hyprlang", "toml", "ini", "css", "scss", "json", "yaml", "qml"}

// nvimConfig is a Neovim 0.11+ native-LSP snippet. Neovim attaches servers
// additively, so prowl-agent runs alongside any per-language server.
const nvimConfig = `-- prowl-agent language server (Neovim 0.11+).
-- Source this file from your config: dofile(vim.fn.getcwd() .. "/.prowl/editor/nvim.lua")
-- or copy the block into your own config.
vim.lsp.config("prowl_agent", {
  cmd = { "prowl-agent", "lsp" },
  filetypes = {
    "conf", "config", "dosini", "toml", "yaml", "json", "jsonc",
    "css", "scss", "lua", "python", "sh", "bash", "fish", "qml", "hyprlang",
  },
  root_markers = { ".prowl", ".git" },
})
vim.lsp.enable("prowl_agent")
`

const editorSetupDoc = `# Editor setup (prowl-agent LSP)

The same index that powers the MCP server also drives a language server, so your
editor can do go-to-definition (a keybind to its script, an include to its file,
a variable to where it is defined), find-references, hover, document/workspace
symbols, code lens, completion, and inline health checks.

The server is launched by your editor as ` + "`prowl-agent lsp`" + ` and reads
this project's ` + "`.prowl/index.db`" + `. Nothing leaves your machine.

## Neovim (0.11+)

Source the generated snippet from your config:

    dofile(vim.fn.getcwd() .. "/.prowl/editor/nvim.lua")

Or copy ` + "`nvim.lua`" + ` (in this folder) into your config. Neovim attaches
servers additively, so this runs alongside your existing language servers.

## Helix

If this project had no ` + "`.helix/languages.toml`" + `, init wrote one wiring
prowl-agent for common config formats. Otherwise, merge ` + "`helix-languages.toml`" + `
(in this folder) into ` + "`.helix/languages.toml`" + ` or ` + "`~/.config/helix/languages.toml`" + `.
Helix replaces the per-language server list, so add your existing servers back to
any language you also use another server for.

## VS Code

VS Code needs a small extension to launch a custom language server; the MCP
integration (` + "`.vscode/mcp.json`" + `) is wired already. LSP support is on the
roadmap.
`

// helixConfig renders a project/global Helix languages.toml registering the
// prowl-agent language server for the config formats above.
func helixConfig() string {
	var b strings.Builder
	b.WriteString("[language-server.prowl-agent]\ncommand = \"prowl-agent\"\nargs = [\"lsp\"]\n\n")
	b.WriteString("# Helix replaces the per-language server list, so add your existing servers\n")
	b.WriteString("# back to any language below if you rely on them.\n")
	for _, l := range helixLangs {
		fmt.Fprintf(&b, "\n[[language]]\nname = \"%s\"\nlanguage-servers = [\"prowl-agent\"]\n", l)
	}
	return b.String()
}

// InjectEditor writes the editor LSP setup as part of init: generated configs
// under .prowl/editor/, plus a project-local .helix/languages.toml when there is
// none to clobber. It never edits a user's existing editor config.
func InjectEditor(root string) error {
	dir := filepath.Join(root, workspace.Dir, "editor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"nvim.lua":             nvimConfig,
		"helix-languages.toml": helixConfig(),
		"SETUP.md":             editorSetupDoc,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	helix := filepath.Join(root, ".helix", "languages.toml")
	if _, err := os.Stat(helix); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(helix), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(helix, []byte(helixConfig()), 0o644); err != nil {
			return err
		}
	}
	return nil
}
