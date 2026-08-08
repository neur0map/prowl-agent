package extract

import "testing"

func TestQMLUsesEdges(t *testing.T) {
	src := "import QtQuick\n" +
		"Item {\n" +
		"  Button { }\n" +
		"  width: Config.spacing\n" +
		"  color: Theme.accent.value\n" +
		"  height: parent.height\n" +
		"}\n"
	r := mustExtract(t, "qml", src)

	if !has(edgeRaws(r, "instantiates"), "Button") {
		t.Fatalf("instantiates=%v want Button", edgeRaws(r, "instantiates"))
	}

	uses := edgeRaws(r, "uses")
	if !has(uses, "Config") || !has(uses, "Theme") {
		t.Fatalf("uses=%v want Config and Theme (capitalized singleton refs)", uses)
	}
	if has(uses, "parent") {
		t.Fatalf("uses=%v must exclude lowercase member access like parent", uses)
	}
}
