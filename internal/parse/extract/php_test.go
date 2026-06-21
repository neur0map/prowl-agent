package extract

import "testing"

func TestPHPExtractor(t *testing.T) {
	src := "<?php\n" +
		"namespace App\\Service;\n" +
		"use App\\Model\\User;\n" +
		"use App\\Repo\\UserRepo as Repo;\n" +
		"require_once __DIR__ . '/helpers.php';\n" +
		"interface Greeter { public function greet(): string; }\n" +
		"trait Logs { public function log($m) {} }\n" +
		"enum Status { case On; case Off; }\n" +
		"class Mailer implements Greeter {\n" +
		"  public function send(int $x): bool {\n" +
		"    if ($x > 0) { foreach ([1,2] as $i) {} }\n" +
		"    return true;\n" +
		"  }\n" +
		"}\n" +
		"function helper($a) { return $a; }\n"
	r := mustExtract(t, "php", src)

	for _, c := range []struct{ kind, name string }{
		{"class", "Mailer"}, {"interface", "Greeter"}, {"trait", "Logs"},
		{"enum", "Status"}, {"function", "helper"}, {"method", "send"},
	} {
		if !has(symNames(r, c.kind), c.name) {
			t.Errorf("%s symbols = %v, want %q", c.kind, symNames(r, c.kind), c.name)
		}
	}

	// use imports are recorded as fully-qualified class names; the alias is not a
	// separate edge. require resolves to the concatenated path literal.
	if !has(edgeRaws(r, "includes"), "App\\Model\\User") {
		t.Errorf("imports = %v, want App\\Model\\User", edgeRaws(r, "includes"))
	}
	if !has(edgeRaws(r, "includes"), "App\\Repo\\UserRepo") {
		t.Errorf("imports = %v, want App\\Repo\\UserRepo", edgeRaws(r, "includes"))
	}
	if has(edgeRaws(r, "includes"), "Repo") {
		t.Errorf("alias name should not be an import edge: %v", edgeRaws(r, "includes"))
	}
	if !has(edgeRaws(r, "includes"), "/helpers.php") {
		t.Errorf("require path = %v, want /helpers.php", edgeRaws(r, "includes"))
	}

	// send: if + foreach = 2 decisions -> complexity 3.
	for _, s := range r.Symbols {
		if s.Name == "send" && s.Complexity != 3 {
			t.Errorf("send complexity = %d, want 3", s.Complexity)
		}
	}

	// the file's namespace is recorded for resolution.
	var gotNS bool
	for _, res := range r.Resources {
		if res.Kind == "namespace" && res.Name == "App\\Service" {
			gotNS = true
		}
	}
	if !gotNS {
		t.Errorf("namespace resource App\\Service not recorded: %+v", r.Resources)
	}
}

func TestPHPGroupUse(t *testing.T) {
	src := "<?php\nnamespace App;\nuse App\\Model\\{User, Post};\n"
	r := mustExtract(t, "php", src)
	for _, want := range []string{"App\\Model\\User", "App\\Model\\Post"} {
		if !has(edgeRaws(r, "includes"), want) {
			t.Errorf("group use imports = %v, want %q", edgeRaws(r, "includes"), want)
		}
	}
}
