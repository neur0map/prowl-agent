package cli

import (
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestFormatValueTOONTabular(t *testing.T) {
	hits := []store.SymbolHit{
		{ID: 1, Name: "battery", Kind: "widget", File: "bar/battery.lua", Line: 12},
		{ID: 2, Name: "battery_low", Kind: "function", File: "bar/battery.lua", Line: 40},
	}
	got, err := formatValue(hits, formatTOON)
	if err != nil {
		t.Fatalf("formatValue toon: %v", err)
	}
	// Uniform array of structs must collapse to one header + CSV-style rows,
	// (fields alphabetized via the json round-trip, named once in the header).
	if !strings.Contains(got, "{file,id,kind,line,name}") {
		t.Errorf("expected tabular header naming the columns, got:\n%s", got)
	}
	if !strings.Contains(got, "bar/battery.lua,1,widget,12,battery") {
		t.Errorf("expected a CSV-style data row in header order, got:\n%s", got)
	}
	if strings.Count(got, "name") != 1 {
		t.Errorf("field name should appear once (header only), got:\n%s", got)
	}
	// signature is omitempty and unset here, so it must be dropped, not emitted.
	if strings.Contains(got, "signature") {
		t.Errorf("omitempty field should be dropped, got:\n%s", got)
	}
}

func TestFormatValueJSONMatchesTags(t *testing.T) {
	hits := []store.SymbolHit{{ID: 1, Name: "x", Kind: "k", File: "f", Line: 1}}
	got, err := formatValue(hits, formatJSON)
	if err != nil {
		t.Fatalf("formatValue json: %v", err)
	}
	if !strings.Contains(got, `"start_line"`) && !strings.Contains(got, `"line"`) {
		t.Errorf("json should use json tags, got: %s", got)
	}
	if strings.Contains(got, "signature") {
		t.Errorf("omitempty signature should be absent, got: %s", got)
	}
}

func TestFormatValueTOONIsLeanerThanJSON(t *testing.T) {
	hits := make([]store.SymbolHit, 0, 20)
	for i := 0; i < 20; i++ {
		hits = append(hits, store.SymbolHit{ID: int64(i), Name: "sym", Kind: "function", File: "some/path/file.lua", Line: i})
	}
	js, _ := formatValue(hits, formatJSON)
	tn, _ := formatValue(hits, formatTOON)
	if len(tn) >= len(js) {
		t.Errorf("TOON (%d bytes) should be leaner than JSON (%d bytes) for uniform arrays", len(tn), len(js))
	}
}
