package extract

import "testing"

func TestKotlinExtractor(t *testing.T) {
	src := "package com.foo.bar\n" +
		"import com.baz.Qux\n" +
		"import com.baz.util.*\n" +
		"import com.baz.Helper as H\n" +
		"interface Greeter { fun greet(): String }\n" +
		"object Singleton { fun run() {} }\n" +
		"enum class Status { ON, OFF }\n" +
		"class Mailer(val x: Int) : Greeter {\n" +
		"  fun send(n: Int): Boolean {\n" +
		"    if (n > 0) { for (i in 0..n) {} }\n" +
		"    return true\n" +
		"  }\n" +
		"}\n" +
		"fun helper(a: Int): Int { return a }\n"
	r := mustExtract(t, "kotlin", src)

	if !has(symNames(r, "class"), "Mailer") {
		t.Errorf("classes = %v, want Mailer", symNames(r, "class"))
	}
	if !has(symNames(r, "object"), "Singleton") {
		t.Errorf("objects = %v, want Singleton", symNames(r, "object"))
	}
	if !has(symNames(r, "enum"), "Status") {
		t.Errorf("enums = %v, want Status", symNames(r, "enum"))
	}
	if !has(symNames(r, "function"), "send") || !has(symNames(r, "function"), "helper") {
		t.Errorf("functions = %v, want send and helper", symNames(r, "function"))
	}

	// non-wildcard, wildcard, and aliased imports are all FQCN edges; the wildcard
	// keeps a trailing .* (matching the Java wildcard form). The alias is not an edge.
	for _, want := range []string{"com.baz.Qux", "com.baz.util.*"} {
		if !has(edgeRaws(r, "includes"), want) {
			t.Errorf("imports = %v, want %q", edgeRaws(r, "includes"), want)
		}
	}
	if has(edgeRaws(r, "includes"), "H") {
		t.Errorf("alias should not be an import edge: %v", edgeRaws(r, "includes"))
	}

	// send: if + for = 2 decisions -> complexity 3.
	for _, s := range r.Symbols {
		if s.Name == "send" && s.Complexity != 3 {
			t.Errorf("send complexity = %d, want 3", s.Complexity)
		}
	}
}
