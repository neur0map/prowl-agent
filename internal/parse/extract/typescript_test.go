package extract

import "testing"

func TestTypeScriptExtractor(t *testing.T) {
	src := "import { useState } from \"react\";\n\n" +
		"export interface User { id: number }\n" +
		"export type ID = string;\n" +
		"export enum Role { Admin, User }\n" +
		"export class Service { run(): void {} }\n" +
		"export function make(): Service { return new Service(); }\n" +
		"export const App = () => null;\n"
	for _, lang := range []string{"typescript", "tsx"} {
		r := mustExtract(t, lang, src)
		if !has(symNames(r, "interface"), "User") {
			t.Fatalf("%s interfaces=%v want User", lang, symNames(r, "interface"))
		}
		if !has(symNames(r, "type"), "ID") {
			t.Fatalf("%s types=%v want ID", lang, symNames(r, "type"))
		}
		if !has(symNames(r, "enum"), "Role") {
			t.Fatalf("%s enums=%v want Role", lang, symNames(r, "enum"))
		}
		if !has(symNames(r, "class"), "Service") {
			t.Fatalf("%s classes=%v want Service", lang, symNames(r, "class"))
		}
		if !has(symNames(r, "function"), "make") || !has(symNames(r, "function"), "App") {
			t.Fatalf("%s functions=%v want make and App (arrow const)", lang, symNames(r, "function"))
		}
		if !has(edgeRaws(r, "includes"), "react") {
			t.Fatalf("%s imports=%v want react", lang, edgeRaws(r, "includes"))
		}
	}

	// The tsx grammar also parses JSX, where plain TypeScript would not.
	r := mustExtract(t, "tsx", "export const Btn = () => <button>hi</button>;\n")
	if !has(symNames(r, "function"), "Btn") {
		t.Fatalf("tsx JSX component not found: %v", symNames(r, "function"))
	}
}
