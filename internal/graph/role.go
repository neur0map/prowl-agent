package graph

import (
	"path"
	"strings"
)

// InferRole classifies a file's purpose in a rice from its path and language.
func InferRole(relPath, lang string) string {
	p := strings.ToLower(relPath)
	base := strings.ToLower(path.Base(relPath))
	switch {
	case strings.Contains(p, "hypr/") || strings.Contains(p, "sway/") || strings.Contains(p, "i3/") || base == "hyprland.conf":
		return "wm-config"
	case strings.Contains(p, "waybar/") || strings.Contains(p, "polybar/") || strings.Contains(p, "eww/") || strings.Contains(p, "ags/"):
		return "bar"
	case strings.Contains(p, "rofi/") || strings.HasSuffix(p, ".rasi"):
		return "launcher"
	case strings.Contains(p, "dunst") || strings.Contains(p, "mako"):
		return "notifications"
	case base == "kitty.conf" || base == "alacritty.toml" || strings.Contains(p, "kitty/") ||
		strings.Contains(p, "alacritty/") || strings.Contains(p, "foot/") || strings.Contains(p, "wezterm"):
		return "terminal"
	case lang == "qml":
		return "widget"
	case lang == "css" || lang == "scss" || strings.Contains(p, "gtk") || strings.Contains(p, "theme"):
		return "theme"
	case lang == "lua" && strings.Contains(p, "nvim"):
		return "editor"
	case lang == "bash" || strings.Contains(p, "scripts/") || strings.HasSuffix(p, ".sh"):
		return "script"
	default:
		return ""
	}
}
