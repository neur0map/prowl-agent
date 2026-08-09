package cli

import "testing"

func TestParseAnchorFlags(t *testing.T) {
	anchors, err := parseAnchorFlags([]string{"pkg/foo.go#Foo", "pkg/bar.go:10-20", "pkg/baz.go:42"})
	if err != nil {
		t.Fatal(err)
	}
	if len(anchors) != 3 {
		t.Fatalf("got %d anchors", len(anchors))
	}
	if anchors[0].Path != "pkg/foo.go" || anchors[0].Symbol != "Foo" {
		t.Errorf("symbol anchor: %+v", anchors[0])
	}
	if anchors[1].LineStart != 10 || anchors[1].LineEnd != 20 || anchors[1].Symbol != "" {
		t.Errorf("range anchor: %+v", anchors[1])
	}
	if anchors[2].LineStart != 42 || anchors[2].LineEnd != 42 {
		t.Errorf("single-line anchor: %+v", anchors[2])
	}
	for _, bad := range []string{"pkg/foo.go", "#Foo", "pkg/foo.go:", "pkg/foo.go:x-y", "pkg/foo.go#"} {
		if _, err := parseAnchorFlags([]string{bad}); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
