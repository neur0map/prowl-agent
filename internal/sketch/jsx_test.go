package sketch

import (
	"strings"
	"testing"
)

const sampleJSX = `const Card = ({ title }: Props) => {
    if (loading) return <div className="spin" />;
    return (
        <div className="flex p-4" id="card">
            <h2 className="text-sm">{title}</h2>
            <Button onClick={() => go()}>OK</Button>
            <img src="x.png" />
        </div>
    );
};
`

func TestExtractJSX(t *testing.T) {
	m, err := Of("Card.tsx", []byte(sampleJSX))
	if err != nil {
		t.Fatal(err)
	}
	sk, ok := m.(*Sketch)
	if !ok {
		t.Fatalf("Of returned %T, want *Sketch", m)
	}
	if sk.Kind != "Card (React)" {
		t.Errorf("kind = %q", sk.Kind)
	}
	// The larger return is primary; the guard's <div className="spin"/> is a variant.
	if sk.Root.Kind != "div" || sk.Root.ID != "card" {
		t.Fatalf("root = %+v", sk.Root)
	}
	if len(sk.Variants) != 1 || sk.Variants[0].Kind != "div" {
		t.Fatalf("variants = %+v", sk.Variants)
	}
	if propByName(sk.Root, "className") != "flex p-4" {
		t.Errorf("root className = %q", propByName(sk.Root, "className"))
	}
	// Children: h2 (text), Button (handler + text), img (self-closing).
	h2 := childOfKind(sk.Root, "h2")
	if h2 == nil || propByName(h2, "text") != "{title}" {
		t.Errorf("h2 = %+v", h2)
	}
	btn := childOfKind(sk.Root, "Button")
	if btn == nil || !hasBehavior(btn, "onClick") || propByName(btn, "text") != "OK" {
		t.Errorf("Button = %+v", btn)
	}
	if img := childOfKind(sk.Root, "img"); img == nil || propByName(img, "src") != "x.png" {
		t.Errorf("img = %+v", img)
	}
	// Event handlers must be behavior, not props.
	for _, p := range btn.Props {
		if strings.HasPrefix(p.Name, "on") {
			t.Errorf("handler leaked into props: %s", p.Name)
		}
	}
}

func TestJSXMappedListSurfacesItem(t *testing.T) {
	// An element produced inside {items.map(...)} should surface as a child.
	src := `const List = () => (
        <ul className="grid">
            {items.map((i) => (
                <li key={i.id} className="row">{i.name}</li>
            ))}
        </ul>
    );`
	m, err := Of("List.tsx", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	sk := m.(*Sketch)
	li := childOfKind(sk.Root, "li")
	if li == nil {
		t.Fatalf("mapped <li> not surfaced as child of %+v", sk.Root)
	}
	if propByName(li, "className") != "row" {
		t.Errorf("li className = %q", propByName(li, "className"))
	}
}

func propByName(n *Node, name string) string {
	for _, p := range n.Props {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}
