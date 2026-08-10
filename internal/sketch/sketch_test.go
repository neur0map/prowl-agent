package sketch

import (
	"strings"
	"testing"
)

const sampleQML = `// A labeled toggle row.
import QtQuick
Rectangle {
    id: root
    width: 200
    height: 40
    color: "#1e1e2e"
    radius: 6
    border.width: 1
    border.color: Theme.line
    Row {
        spacing: 8
        anchors.fill: parent
        Text {
            text: "Enable"
            font.pixelSize: 14
            color: Theme.text
        }
        Toggle {
            id: sw
            checked: root.on
            onToggled: root.on = checked
        }
    }
    Behavior on color { ColorAnimation { duration: 120 } }
}
`

func TestOfQMLTree(t *testing.T) {
	m, err := Of("ToggleRow.qml", []byte(sampleQML))
	if err != nil {
		t.Fatal(err)
	}
	sk, ok := m.(*Sketch)
	if !ok {
		t.Fatalf("Of returned %T, want *Sketch", m)
	}
	if sk.Kind != "Rectangle" {
		t.Errorf("root kind = %q, want Rectangle", sk.Kind)
	}
	if sk.Desc != "A labeled toggle row." {
		t.Errorf("desc = %q", sk.Desc)
	}
	if sk.Root.ID != "root" {
		t.Errorf("root id = %q, want root", sk.Root.ID)
	}
	// The root's animation behavior must surface as a behavior note.
	if !hasBehavior(sk.Root, "Behavior on color") {
		t.Errorf("missing Behavior-on note; behavior=%v", sk.Root.Behavior)
	}
	// Row -> Text and Toggle children.
	row := childOfKind(sk.Root, "Row")
	if row == nil {
		t.Fatalf("no Row child")
	}
	if childOfKind(row, "Text") == nil || childOfKind(row, "Toggle") == nil {
		t.Fatalf("Row missing Text/Toggle children: %v", kinds(row.Children))
	}
	// The signal handler must be behavior, not a visual prop.
	sw := childOfKind(row, "Toggle")
	if !hasBehavior(sw, "onToggled") {
		t.Errorf("Toggle missing onToggled behavior: %v", sw.Behavior)
	}
	for _, p := range sw.Props {
		if strings.HasPrefix(p.Name, "on") {
			t.Errorf("handler leaked into props: %s", p.Name)
		}
	}
}

func TestPropGrouping(t *testing.T) {
	cases := map[string]string{
		"width": "geom", "radius": "geom",
		"anchors.fill": "layout", "spacing": "layout", "Layout.fillWidth": "layout",
		"color": "paint", "border.width": "paint", "visible": "paint",
		"text": "text", "font.pixelSize": "text", "elide": "text",
		"onClicked": "other",
	}
	for name, want := range cases {
		if got := classify(name); got != want {
			t.Errorf("classify(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestClipValue(t *testing.T) {
	if got := clip("parent;"); got != "parent" {
		t.Errorf("trailing semicolon not stripped: %q", got)
	}
	long := strings.Repeat("x", 100)
	got := clip(long)
	if len([]rune(got)) != 60 || !strings.HasSuffix(got, "…") {
		t.Errorf("clip did not cap long value: len=%d suffix ok=%v", len([]rune(got)), strings.HasSuffix(got, "…"))
	}
}

func TestQMLDeclaredProperties(t *testing.T) {
	const qml = `pragma Singleton
import QtQuick
Singleton {
    readonly property color brand: "#e2342a"   // one vermillion
    property int radius: 8
    property real spacing
}
`
	m, err := Of("Theme.qml", []byte(qml))
	if err != nil {
		t.Fatal(err)
	}
	sk := m.(*Sketch)
	want := []Decl{
		{Name: "brand", Type: "color", Value: `"#e2342a"`}, // trailing comment stripped
		{Name: "radius", Type: "int", Value: "8"},
		{Name: "spacing", Type: "real", Value: ""}, // no value
	}
	if len(sk.Root.Decls) != len(want) {
		t.Fatalf("decls = %+v, want %d", sk.Root.Decls, len(want))
	}
	for i, w := range want {
		if sk.Root.Decls[i] != w {
			t.Errorf("decl[%d] = %+v, want %+v", i, sk.Root.Decls[i], w)
		}
	}
	if !strings.Contains(sk.Text(), `• brand: color = "#e2342a"`) {
		t.Errorf("declared property not rendered:\n%s", sk.Text())
	}
}

func TestTextRenderContract(t *testing.T) {
	txt, err := Render("ToggleRow.qml", []byte(sampleQML))
	if err != nil {
		t.Fatal(err)
	}
	// Header line names the file and root kind.
	if !strings.HasPrefix(txt, "ToggleRow.qml  ·  Rectangle") {
		t.Errorf("header wrong: %q", firstLine(txt))
	}
	// Behavior notes render with the ~ marker; children are indented.
	if !strings.Contains(txt, "~ onToggled") {
		t.Errorf("handler not rendered:\n%s", txt)
	}
	if !strings.Contains(txt, "border(width=1 color=Theme.line)") {
		t.Errorf("border family not grouped:\n%s", txt)
	}
}

const sampleGo = `package main
import "charm.land/lipgloss/v2"
const hexAccent = "#89b4fa"
var (
	cAccent = lipgloss.Color(hexAccent)
	cText   = lipgloss.Color("#cdd6f4")
	stTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent).Padding(0, 2)
	box     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cText).Bold(false)
)
`

func TestOfGoUI(t *testing.T) {
	m, err := Of("style.go", []byte(sampleGo))
	if err != nil {
		t.Fatal(err)
	}
	ui, ok := m.(*GoUI)
	if !ok {
		t.Fatalf("Of returned %T, want *GoUI", m)
	}
	// Palette: the hex constant is resolved to its literal value.
	pal := map[string]string{}
	for _, c := range ui.Palette {
		pal[c.Name] = c.Value
	}
	if pal["cAccent"] != "#89b4fa" {
		t.Errorf("cAccent = %q, want #89b4fa (const unresolved)", pal["cAccent"])
	}
	if pal["cText"] != "#cdd6f4" {
		t.Errorf("cText = %q", pal["cText"])
	}
	// Styles: attributes are extracted; Bold(false) is dropped.
	st := styleByName(ui, "stTitle")
	if st == nil || !attrHas(st, "bold", "") || !attrHas(st, "fg", "cAccent") {
		t.Errorf("stTitle attrs wrong: %+v", st)
	}
	box := styleByName(ui, "box")
	if box == nil || !attrHas(box, "border", "lipgloss.RoundedBorder()") || attrHas(box, "bold", "") {
		t.Errorf("box attrs wrong (Bold(false) should be dropped): %+v", box)
	}
	// Text render resolves color references inline.
	if !strings.Contains(ui.Text(), "fg=cAccent⟨#89b4fa⟩") {
		t.Errorf("style color not resolved in text:\n%s", ui.Text())
	}
}

func TestUnsupportedAndEmpty(t *testing.T) {
	if _, err := Of("notes.txt", []byte("hello")); err == nil {
		t.Error("expected error for unsupported file type")
	}
	if _, err := Of("plain.go", []byte("package main\nfunc main() {}\n")); err == nil {
		t.Error("expected error for a Go file with no lipgloss UI")
	}
}

func styleByName(ui *GoUI, name string) *NamedStyle {
	for i := range ui.Styles {
		if ui.Styles[i].Name == name {
			return &ui.Styles[i]
		}
	}
	return nil
}

func attrHas(s *NamedStyle, name, val string) bool {
	for _, a := range s.Attrs {
		if a.Name == name && a.Value == val {
			return true
		}
	}
	return false
}

// helpers

func hasBehavior(n *Node, sub string) bool {
	for _, b := range n.Behavior {
		if strings.Contains(b, sub) {
			return true
		}
	}
	return false
}

func childOfKind(n *Node, kind string) *Node {
	for _, c := range n.Children {
		if c.Kind == kind {
			return c
		}
	}
	return nil
}

func kinds(ns []*Node) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Kind)
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
