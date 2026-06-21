package extract

import "testing"

func TestRustExtractor(t *testing.T) {
	src := "use std::collections::HashMap;\n\n" +
		"pub struct Server { name: String }\n" +
		"pub enum State { On, Off }\n" +
		"pub trait Run { fn run(&self); }\n" +
		"type Id = u64;\n" +
		"impl Server { pub fn new() -> Self { Server { name: String::new() } } }\n" +
		"pub fn main() {}\n"
	r := mustExtract(t, "rust", src)

	if !has(symNames(r, "struct"), "Server") {
		t.Fatalf("structs=%v want Server", symNames(r, "struct"))
	}
	if !has(symNames(r, "enum"), "State") {
		t.Fatalf("enums=%v want State", symNames(r, "enum"))
	}
	if !has(symNames(r, "trait"), "Run") {
		t.Fatalf("traits=%v want Run", symNames(r, "trait"))
	}
	if !has(symNames(r, "type"), "Id") {
		t.Fatalf("types=%v want Id", symNames(r, "type"))
	}
	if !has(symNames(r, "function"), "new") || !has(symNames(r, "function"), "main") {
		t.Fatalf("functions=%v want new and main", symNames(r, "function"))
	}
	if !has(edgeRaws(r, "includes"), "std::collections::HashMap") {
		t.Fatalf("imports=%v want std::collections::HashMap", edgeRaws(r, "includes"))
	}
}
