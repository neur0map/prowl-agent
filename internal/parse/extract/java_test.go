package extract

import "testing"

func TestJavaExtractor(t *testing.T) {
	src := "package com.foo;\n" +
		"import com.bar.Baz;\n" +
		"import java.util.List;\n" +
		"public class Service {\n" +
		"  interface Run { void go(); }\n" +
		"  enum State { ON, OFF }\n" +
		"  public int handle(int x) { if (x > 0) { for (int i = 0; i < x; i++) {} } return x; }\n" +
		"}\n"
	r := mustExtract(t, "java", src)

	if !has(symNames(r, "class"), "Service") {
		t.Fatalf("classes=%v want Service", symNames(r, "class"))
	}
	if !has(symNames(r, "interface"), "Run") {
		t.Fatalf("interfaces=%v want Run", symNames(r, "interface"))
	}
	if !has(symNames(r, "enum"), "State") {
		t.Fatalf("enums=%v want State", symNames(r, "enum"))
	}
	if !has(symNames(r, "method"), "handle") {
		t.Fatalf("methods=%v want handle", symNames(r, "method"))
	}
	if !has(edgeRaws(r, "includes"), "com.bar.Baz") {
		t.Fatalf("imports=%v want com.bar.Baz", edgeRaws(r, "includes"))
	}
	// handle: if + for = 2 decisions -> complexity 3.
	for _, s := range r.Symbols {
		if s.Name == "handle" && s.Complexity != 3 {
			t.Errorf("handle complexity = %d, want 3", s.Complexity)
		}
	}
}
