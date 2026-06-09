package extract

import "testing"

func TestCppExtractor(t *testing.T) {
	r := mustExtract(t, "cpp", "#include <vector>\n#include \"widget.h\"\nclass Bar {};\nvoid run() {}\n")
	if !has(symNames(r, "class"), "Bar") {
		t.Fatalf("classes=%v", symNames(r, "class"))
	}
	if !has(symNames(r, "function"), "run") {
		t.Fatalf("functions=%v", symNames(r, "function"))
	}
	inc := edgeRaws(r, "includes")
	if !has(inc, "widget.h") {
		t.Fatalf("includes=%v want widget.h (system <vector> excluded)", inc)
	}
	if has(inc, "vector") {
		t.Fatalf("system include should be excluded: %v", inc)
	}
}

func TestFishExtractor(t *testing.T) {
	r := mustExtract(t, "fish", "function greet\n  echo hi\nend\nsource ~/.config/fish/aliases.fish\n")
	if !has(symNames(r, "function"), "greet") {
		t.Fatalf("functions=%v", symNames(r, "function"))
	}
	if !has(edgeRaws(r, "includes"), "~/.config/fish/aliases.fish") {
		t.Fatalf("includes=%v", edgeRaws(r, "includes"))
	}
}
