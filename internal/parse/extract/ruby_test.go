package extract

import "testing"

func TestRubyExtractor(t *testing.T) {
	src := "require_relative 'store'\n" +
		"require 'json'\n" +
		"module Foo\n" +
		"  class Service\n" +
		"    def handle(x)\n" +
		"      return 0 unless x\n" +
		"      if x > 0\n" +
		"        x.times { |i| }\n" +
		"      end\n" +
		"      x > 0 ? 1 : 2\n" +
		"    end\n" +
		"  end\n" +
		"end\n"
	r := mustExtract(t, "ruby", src)

	if !has(symNames(r, "module"), "Foo") {
		t.Fatalf("modules=%v want Foo", symNames(r, "module"))
	}
	if !has(symNames(r, "class"), "Service") {
		t.Fatalf("classes=%v want Service", symNames(r, "class"))
	}
	if !has(symNames(r, "method"), "handle") {
		t.Fatalf("methods=%v want handle", symNames(r, "method"))
	}
	if !has(edgeRaws(r, "includes"), "store") {
		t.Fatalf("requires=%v want store", edgeRaws(r, "includes"))
	}
	// handle: unless_modifier + if + ternary = 3 decisions -> complexity 4.
	for _, s := range r.Symbols {
		if s.Name == "handle" && s.Complexity != 4 {
			t.Errorf("handle complexity = %d, want 4", s.Complexity)
		}
	}
}
