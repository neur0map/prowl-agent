package parse

import "testing"

func TestDetect(t *testing.T) {
	cases := []struct {
		path string
		head string
		want string
	}{
		{"foo.lua", "", "lua"},
		{"a/b/init.py", "", "python"},
		{"scripts/run.sh", "", "bash"},
		{"style.css", "", "css"},
		{"theme.scss", "", "scss"},
		{"waybar/config.json", "", "json"},
		{"alacritty.toml", "", "toml"},
		{"settings.yaml", "", "yaml"},
		{"bar.qml", "", "qml"},
		{"hypr/hyprland.conf", "", "hyprlang"},
		{"colors.rasi", "", "rasi"},
		{"sway/config", "", "generic"},
		{"i3/config", "", "generic"},
		{"picom.conf", "", "generic"},
		{"scripts/noext", "#!/usr/bin/env bash\n", "bash"},
		{"scripts/pyrun", "#!/usr/bin/python3\n", "python"},
		{"widgets/bar.cpp", "", "cpp"},
		{"widgets/theme.hpp", "", "cpp"},
		{"fish/config.fish", "", "fish"},
		{"scripts/frun", "#!/usr/bin/fish\n", "fish"},
		{"README.md", "", "markdown"},
		{"docs/install.mdx", "", "markdown"},
		{"shell/config.js", "", "javascript"},
		{"app.mjs", "", "javascript"},
		{"random", "", ""},
	}
	for _, c := range cases {
		if got := Detect(c.path, []byte(c.head)); got != c.want {
			t.Errorf("Detect(%q,%q)=%q want %q", c.path, c.head, got, c.want)
		}
	}
}
