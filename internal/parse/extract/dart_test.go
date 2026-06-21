package extract

import "testing"

func TestDartExtractor(t *testing.T) {
	src := "import 'dart:async';\n" +
		"import 'package:flutter/material.dart';\n" +
		"import 'package:myapp/models/user.dart';\n" +
		"import '../util/helpers.dart';\n" +
		"export 'src/api.dart';\n" +
		"class Mailer extends Base {\n" +
		"  Future<bool> send(int n) async {\n" +
		"    if (n > 0) { for (var i = 0; i < n; i++) {} }\n" +
		"    return true;\n" +
		"  }\n" +
		"}\n" +
		"mixin Logs { void log(String m) {} }\n" +
		"enum Status { on, off }\n" +
		"extension StringX on String { String shout() => toUpperCase(); }\n" +
		"void helper(int a) { return; }\n"
	r := mustExtract(t, "dart", src)

	for _, c := range []struct{ kind, name string }{
		{"class", "Mailer"}, {"mixin", "Logs"}, {"enum", "Status"},
		{"extension", "StringX"}, {"function", "send"}, {"function", "helper"},
	} {
		if !has(symNames(r, c.kind), c.name) {
			t.Errorf("%s symbols = %v, want %q", c.kind, symNames(r, c.kind), c.name)
		}
	}

	// All directive URIs become include edges (import, export, package, relative).
	for _, want := range []string{"dart:async", "package:myapp/models/user.dart", "../util/helpers.dart", "src/api.dart"} {
		if !has(edgeRaws(r, "includes"), want) {
			t.Errorf("imports = %v, want %q", edgeRaws(r, "includes"), want)
		}
	}

	// send: if + for = 2 decisions -> complexity 3 (body is the signature's sibling).
	for _, s := range r.Symbols {
		if s.Name == "send" && s.Complexity != 3 {
			t.Errorf("send complexity = %d, want 3", s.Complexity)
		}
	}
}

// TestDartPartDirectives checks that a `part` directive becomes an include edge
// but its reverse `part of` does not (which would fake a cycle with a generated
// companion file).
func TestDartPartDirectives(t *testing.T) {
	lib := mustExtract(t, "dart", "import 'other.dart';\npart 'model.g.dart';\n")
	if !has(edgeRaws(lib, "includes"), "model.g.dart") {
		t.Errorf("part directive missing: %v", edgeRaws(lib, "includes"))
	}
	if !has(edgeRaws(lib, "includes"), "other.dart") {
		t.Errorf("import missing: %v", edgeRaws(lib, "includes"))
	}
	part := mustExtract(t, "dart", "part of 'model.dart';\n")
	if has(edgeRaws(part, "includes"), "model.dart") {
		t.Errorf("part of should not emit an edge (fakes a cycle): %v", edgeRaws(part, "includes"))
	}
}
