package okfv01

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCodecRoundTripsUnknownTypeAndFields(t *testing.T) {
	input := []byte("---\r\ntype: FutureConcept\r\ntitle: Original\r\ntags: [one, two]\r\nx-enabled: true\r\nprowl:\r\n  id: future-1\r\n  status: accepted\r\n  future_policy:\r\n    mode: careful\r\n  anchors:\r\n    - path: src/a.go\r\n      line_start: 4\r\n      content_hash: sha256:abc\r\n      x-offset: 9\r\n---\r\nBody with a [broken link](missing.md).\r\n")
	codec := Codec{}
	doc, err := codec.Parse("concepts/future.md", input)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Type != "FutureConcept" || doc.Prowl.ID != "future-1" || len(doc.Prowl.Anchors) != 1 {
		t.Fatalf("known fields not decoded: %+v", doc)
	}
	if !bytes.Contains(doc.Body, []byte("broken link")) {
		t.Fatalf("body lost: %q", doc.Body)
	}
	doc.Title = "Updated"
	encoded, err := codec.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := codec.Parse(doc.Path, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.Title != "Updated" || roundTrip.Type != "FutureConcept" || !reflect.DeepEqual(roundTrip.Tags, []string{"one", "two"}) {
		t.Fatalf("semantic round trip failed: %+v", roundTrip)
	}
	mapping, err := mappingNode(roundTrip.Frontmatter)
	if err != nil {
		t.Fatal(err)
	}
	if n := valueNode(mapping, "x-enabled"); n == nil || n.Tag != "!!bool" || n.Value != "true" {
		t.Fatalf("unknown typed field lost: %#v", n)
	}
	p := valueNode(mapping, "prowl")
	if future := valueNode(p, "future_policy"); future == nil || scalar(future, "mode") != "careful" {
		t.Fatalf("unknown prowl field lost: %#v", future)
	}
	anchors := valueNode(p, "anchors")
	if anchors == nil || len(anchors.Content) != 1 || scalar(anchors.Content[0], "x-offset") != "9" {
		t.Fatalf("unknown anchor field lost: %#v", anchors)
	}
}

func TestCodecReservedDocumentsMayOmitType(t *testing.T) {
	codec := Codec{}
	for _, path := range []string{"index.md", "nested/log.md"} {
		doc, err := codec.Parse(path, []byte("# Reserved\n"))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if doc.Type != "" || !bytes.Equal(doc.Body, []byte("# Reserved\n")) {
			t.Fatalf("unexpected reserved document: %+v", doc)
		}
	}
}

func TestCodecRejectsMissingTypeMalformedYAMLAndUnsafePaths(t *testing.T) {
	codec := Codec{}
	cases := []struct {
		path string
		data []byte
		want string
	}{
		{"concept.md", []byte("---\ntitle: Missing type\n---\n"), "required frontmatter"},
		{"concept.md", []byte("---\ntitle: [broken\n---\n"), "invalid YAML"},
		{"../outside.md", []byte("---\ntype: Note\n---\n"), "unsafe knowledge path"},
		{"/absolute.md", []byte("---\ntype: Note\n---\n"), "unsafe knowledge path"},
	}
	for _, tc := range cases {
		if _, err := codec.Parse(tc.path, tc.data); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q) error = %v, want containing %q", tc.path, err, tc.want)
		}
	}
}

func TestMarshalNewDocumentIncludesProwlMetadata(t *testing.T) {
	codec := Codec{}
	doc, err := codec.Parse("decision.md", []byte("---\ntype: Decision\n---\nDecision body.\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc.Prowl.ID = "decision-1"
	doc.Prowl.Status = "accepted"
	doc.Prowl.Related = []string{"architecture/storage"}
	encoded, err := codec.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("id: decision-1")) || !bytes.Contains(encoded, []byte("status: accepted")) {
		t.Fatalf("metadata missing:\n%s", encoded)
	}
}

func TestCodecReadsGoogleAppendixAFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "google-appendix-a-orders.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := (Codec{}).Parse("tables/orders.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Type != "BigQuery Table" || doc.Title != "Orders" || doc.Timestamp != "2026-05-28T00:00:00Z" || !reflect.DeepEqual(doc.Tags, []string{"sales", "orders"}) {
		t.Fatalf("official fixture decoded incorrectly: %+v", doc)
	}
	encoded, err := (Codec{}).Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := (Codec{}).Parse(doc.Path, encoded)
	if err != nil || roundTrip.Resource != doc.Resource || !bytes.Equal(roundTrip.Body, doc.Body) {
		t.Fatalf("official fixture round trip failed: %+v, %v", roundTrip, err)
	}
}
