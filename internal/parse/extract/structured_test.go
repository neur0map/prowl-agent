package extract

import "testing"

func TestStructuredJSON(t *testing.T) {
	r := mustExtract(t, "json", "{\n  \"layer\": \"top\",\n  \"modules\": [\"clock\"],\n  \"on-click\": \"~/.config/waybar/x.sh\"\n}\n")
	for _, k := range []string{"layer", "modules", "on-click"} {
		if !has(symNames(r, "setting"), k) {
			t.Fatalf("missing setting %q in %v", k, symNames(r, "setting"))
		}
	}
	if !has(edgeRaws(r, "references"), "~/.config/waybar/x.sh") {
		t.Fatalf("references=%v", edgeRaws(r, "references"))
	}
}

func TestStructuredTOMLandINI(t *testing.T) {
	toml := mustExtract(t, "toml", "[window]\nopacity = 0.9\npath = \"~/.config/x.sh\"\n")
	if !has(symNames(toml, "config_section"), "window") {
		t.Fatalf("toml sections=%v", symNames(toml, "config_section"))
	}
	if !has(edgeRaws(toml, "references"), "~/.config/x.sh") {
		t.Fatalf("toml refs=%v", edgeRaws(toml, "references"))
	}
	ini := mustExtract(t, "ini", "[bar]\nmodules-left=clock\ncommand=/usr/bin/foo\n")
	if !has(symNames(ini, "config_section"), "bar") || !has(symNames(ini, "setting"), "command") {
		t.Fatalf("ini syms sec=%v set=%v", symNames(ini, "config_section"), symNames(ini, "setting"))
	}
	if !has(edgeRaws(ini, "references"), "/usr/bin/foo") {
		t.Fatalf("ini refs=%v", edgeRaws(ini, "references"))
	}
}

func TestSCSSExtractor(t *testing.T) {
	r := mustExtract(t, "scss", "$accent: #1e1e2e;\n.box {\n  color: $accent;\n  background: #fff;\n}\n@import \"colors\";\n")
	if !has(edgeRaws(r, "declares_resource"), "$accent") {
		t.Fatalf("declares=%v", edgeRaws(r, "declares_resource"))
	}
	if !has(edgeRaws(r, "uses_resource"), "$accent") {
		t.Fatalf("uses=%v", edgeRaws(r, "uses_resource"))
	}
	if !has(edgeRaws(r, "includes"), "colors") {
		t.Fatalf("includes=%v", edgeRaws(r, "includes"))
	}
}

func TestGenericSway(t *testing.T) {
	src := "set $mod Mod4\nbindsym $mod+Return exec kitty\nexec_always waybar\ninclude ~/.config/sway/colors\nfont pango:DejaVu 10\n"
	r := mustExtract(t, "generic", src)
	if !has(edgeRaws(r, "declares_resource"), "$mod") {
		t.Fatalf("declares=%v", edgeRaws(r, "declares_resource"))
	}
	if !has(symNames(r, "keybind"), "$mod+Return") {
		t.Fatalf("keybinds=%v", symNames(r, "keybind"))
	}
	if !has(edgeRaws(r, "binds"), "kitty") {
		t.Fatalf("binds=%v", edgeRaws(r, "binds"))
	}
	if !has(edgeRaws(r, "execs"), "waybar") {
		t.Fatalf("execs=%v", edgeRaws(r, "execs"))
	}
	if !has(edgeRaws(r, "includes"), "~/.config/sway/colors") {
		t.Fatalf("includes=%v", edgeRaws(r, "includes"))
	}
}

func TestGenericRasi(t *testing.T) {
	src := "* {\n  background: #1e1e2e;\n  text-color: @foreground;\n}\n@import \"shared.rasi\"\n"
	r := mustExtract(t, "rasi", src)
	if !has(edgeRaws(r, "uses_resource"), "@foreground") {
		t.Fatalf("uses=%v", edgeRaws(r, "uses_resource"))
	}
	if !has(edgeRaws(r, "includes"), "shared.rasi") {
		t.Fatalf("includes=%v", edgeRaws(r, "includes"))
	}
}
