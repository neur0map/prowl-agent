package sketch

import (
	"fmt"
	"strings"
)

// Text renders a scene-tree sketch as an indented, token-lean tree. When a
// sketch has more than one top-level tree (React conditional renders), each is
// rendered under a numbered separator.
func (sk *Sketch) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  ·  %s\n", sk.File, sk.Kind)
	if sk.Desc != "" {
		fmt.Fprintf(&b, "%s\n", sk.Desc)
	}
	b.WriteString("\n")
	roots := append([]*Node{sk.Root}, sk.Variants...)
	for i, r := range roots {
		if r == nil {
			continue
		}
		if len(roots) > 1 {
			fmt.Fprintf(&b, "── render %d of %d ──\n", i+1, len(roots))
		}
		renderNode(&b, r, 0)
		if len(roots) > 1 {
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// renderNode writes one element and its subtree: the element line (kind, id,
// inline visual properties), then declared properties, behavior notes, and
// children, each indented one level deeper.
func renderNode(b *strings.Builder, n *Node, depth int) {
	indent := strings.Repeat("  ", depth)
	head := n.Kind
	if n.Slot != "" {
		head = n.Slot + ": " + head
	}
	if n.ID != "" {
		head += " #" + n.ID
	}
	if props := inlineProps(n); props != "" {
		head += "  " + props
	}
	fmt.Fprintf(b, "%s%s\n", indent, head)
	for _, d := range n.Decls {
		line := "• " + d.Name
		if d.Type != "" {
			line += ": " + d.Type
		}
		if d.Value != "" {
			line += " = " + d.Value
		}
		fmt.Fprintf(b, "%s  %s\n", indent, line)
	}
	for _, bh := range n.Behavior {
		fmt.Fprintf(b, "%s  ~ %s\n", indent, bh)
	}
	for _, c := range n.Children {
		renderNode(b, c, depth+1)
	}
}

// inlineProps formats an element's visual properties compactly, grouping the
// common dotted QML families (anchors, font, border) so the line stays readable.
func inlineProps(n *Node) string {
	var parts []string
	var anchors, font, border []string
	for _, p := range n.Props {
		switch {
		case strings.HasPrefix(p.Name, "anchors."):
			anchors = append(anchors, strings.TrimPrefix(p.Name, "anchors.")+"="+propVal(p))
		case strings.HasPrefix(p.Name, "font."):
			font = append(font, strings.TrimPrefix(p.Name, "font.")+"="+propVal(p))
		case strings.HasPrefix(p.Name, "border."):
			border = append(border, strings.TrimPrefix(p.Name, "border.")+"="+propVal(p))
		default:
			parts = append(parts, p.Name+"="+propVal(p))
		}
	}
	if len(border) > 0 {
		parts = append(parts, "border("+strings.Join(border, " ")+")")
	}
	if len(anchors) > 0 {
		parts = append(parts, "anchors("+strings.Join(anchors, " ")+")")
	}
	if len(font) > 0 {
		parts = append(parts, "font("+strings.Join(font, " ")+")")
	}
	return strings.Join(parts, "  ")
}

// propVal renders a property's value, appending the resolved literal when a
// token reference was resolved (e.g. Tokens.ink⟨#cdd6f4⟩).
func propVal(p Prop) string {
	if p.Resolved != "" {
		return p.Value + "⟨" + p.Resolved + "⟩"
	}
	return p.Value
}
