// Package parse loads Tree-sitter grammars and detects file languages.
package parse

import (
	"bytes"
	"path/filepath"
	"strings"
)

// Detect returns the canonical language id for a file, or "" if unsupported.
// relPath is relative to the project root; head is the first bytes of the file
// (for shebang sniffing) and may be empty.
//
// Language ids: lua, python, bash, css, scss, json, yaml, toml, ini, qml,
// hyprlang (real grammars); rasi, generic (handled by the line-oriented
// generic extractor).
func Detect(relPath string, head []byte) string {
	base := filepath.Base(relPath)
	ext := strings.ToLower(filepath.Ext(base))
	lower := strings.ToLower(relPath)

	// Bespoke WM config family by path marker (often generic basenames).
	switch {
	case strings.Contains(lower, "hypr/") && (ext == ".conf" || base == "hyprland.conf"):
		return "hyprlang"
	case ext == ".rasi":
		return "rasi"
	}

	switch ext {
	case ".lua":
		return "lua"
	case ".py":
		return "python"
	case ".sh", ".bash", ".zsh":
		return "bash"
	case ".css":
		return "css"
	case ".scss":
		return "scss"
	case ".json", ".jsonc":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".ini":
		return "ini"
	case ".qml":
		return "qml"
	case ".conf":
		return "generic"
	}

	if sb := shebangLang(head); sb != "" {
		return sb
	}

	// Extensionless WM configs (sway/i3 use a file literally named "config").
	if base == "config" && (strings.Contains(lower, "sway/") || strings.Contains(lower, "i3/")) {
		return "generic"
	}
	return ""
}

func shebangLang(head []byte) string {
	if !bytes.HasPrefix(head, []byte("#!")) {
		return ""
	}
	line := head
	if i := bytes.IndexByte(head, '\n'); i >= 0 {
		line = head[:i]
	}
	s := string(line)
	switch {
	case strings.Contains(s, "bash"), strings.Contains(s, "/sh"), strings.Contains(s, "zsh"):
		return "bash"
	case strings.Contains(s, "python"):
		return "python"
	case strings.Contains(s, "lua"):
		return "lua"
	}
	return ""
}
