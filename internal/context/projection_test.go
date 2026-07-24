package context

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestCanonicalContextProjection(t *testing.T) {
	packet := Packet{
		SchemaVersion: PacketSchemaVersion,
		Question:      "Which call site owns the token boundary?",
		Summary:       "The authenticated API boundary owns the token.",
		Items: []Item{{
			ID:             "source:api",
			Kind:           "source",
			Title:          "internal/workbench/api.go",
			WhySelected:    []string{"matches authenticated API boundary"},
			Freshness:      "current",
			Confidence:     0.91,
			Audience:       []string{"assistant", "user"},
			Citations:      []Citation{{URI: "prowl://workspace/current/source/internal/workbench/api.go", Path: "internal/workbench/api.go", LineStart: 37, LineEnd: 91}},
			DetailResource: "prowl://workspace/current/source/internal/workbench/api.go",
		}},
		Budget:  Budget{RequestedTokens: 1800, EstimatedTokens: 91, ExactBytes: 512},
		Omitted: map[string]int{"budget": 2},
		Next:    []string{"fetch source:api"},
		TraceID: "volatile-trace-id",
	}

	projected := CanonicalProjection(packet)
	want := packet
	want.TraceID = ""
	if !reflect.DeepEqual(projected, want) {
		t.Fatalf("projection changed durable packet fields:\n got: %#v\nwant: %#v", projected, want)
	}

	encoded, err := MarshalCanonicalProjection(packet)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"trace_id"`)) {
		t.Fatalf("canonical JSON leaked volatile trace: %s", encoded)
	}
	var decoded Packet
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("canonical JSON changed durable packet fields:\n got: %#v\nwant: %#v", decoded, want)
	}
}
