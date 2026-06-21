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
