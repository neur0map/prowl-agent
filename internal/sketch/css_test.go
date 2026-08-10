package sketch

import (
	"strings"
	"testing"
)

const sampleCSS = `:root {
    --accent: #89b4fa;
    --gap: 8px;
}
.card {
    color: var(--accent);
    padding: 4px;
}
`

func TestExtractCSS(t *testing.T) {
	m, err := Of("theme.css", []byte(sampleCSS))
	if err != nil {
		t.Fatal(err)
	}
	sheet, ok := m.(*CSSSheet)
	if !ok {
		t.Fatalf("Of returned %T, want *CSSSheet", m)
	}
	tokens := map[string]string{}
	for _, tk := range sheet.Tokens {
		tokens[tk.Name] = tk.Value
	}
	if tokens["--accent"] != "#89b4fa" || tokens["--gap"] != "8px" {
		t.Errorf("tokens = %+v", sheet.Tokens)
	}
	// :root holds only tokens, so it is not a rule; .card is.
	if len(sheet.Rules) != 1 || sheet.Rules[0].Selector != ".card" {
		t.Fatalf("rules = %+v", sheet.Rules)
	}
	decls := map[string]string{}
	for _, d := range sheet.Rules[0].Decls {
		decls[d.Name] = d.Value
	}
	if decls["color"] != "var(--accent)" || decls["padding"] != "4px" {
		t.Errorf("card decls = %+v", sheet.Rules[0].Decls)
	}
	txt := sheet.Text()
	for _, want := range []string{"TOKENS", "--accent  #89b4fa", "RULES", ".card", "color=var(--accent)"} {
		if !strings.Contains(txt, want) {
			t.Errorf("text missing %q:\n%s", want, txt)
		}
	}
}

func TestExtractSCSSVariables(t *testing.T) {
	// SCSS $variables are tokens too.
	src := "$brand: #e2342a;\n.btn { background: $brand; }\n"
	m, err := Of("style.scss", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	sheet := m.(*CSSSheet)
	found := false
	for _, tk := range sheet.Tokens {
		if tk.Name == "$brand" && tk.Value == "#e2342a" {
			found = true
		}
	}
	if !found {
		t.Errorf("SCSS variable not captured as token: %+v", sheet.Tokens)
	}
}
