package extract

import "testing"

func TestQMLIndexesFunctionsAndSignals(t *testing.T) {
	src := "import QtQuick\nItem {\n  // recompute the charge\n  function computeCharge(x) {\n    return x * 2\n  }\n  signal pressed(int n)\n  property int y: 3\n}\n"
	r, err := qmlExtractor{}.Extract([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var fn, sig *Symbol
	for i := range r.Symbols {
		switch r.Symbols[i].Name {
		case "computeCharge":
			fn = &r.Symbols[i]
		case "pressed":
			sig = &r.Symbols[i]
		}
	}
	if fn == nil || fn.Kind != "function" {
		t.Fatalf("computeCharge not indexed as a function: %+v", r.Symbols)
	}
	// The function must span its whole body so def/read_symbol returns all of it.
	if fn.StartLine != 4 || fn.EndLine < 6 {
		t.Fatalf("function range = %d-%d, want start 4 through the closing brace", fn.StartLine, fn.EndLine)
	}
	if sig == nil || sig.Kind != "signal" {
		t.Fatalf("pressed not indexed as a signal: %+v", r.Symbols)
	}
}
