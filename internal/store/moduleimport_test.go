package store

import "testing"

func TestModuleImportLang(t *testing.T) {
	// Languages whose imports are toolchain module specifiers, so an unresolved
	// import (external dep) must not be flagged as a broken project reference.
	for _, lang := range []string{"go", "rust", "typescript", "tsx", "javascript", "python", "java", "ruby", "csharp", "php", "kotlin", "dart"} {
		if !ModuleImportLang(lang) {
			t.Errorf("ModuleImportLang(%q) = false, want true", lang)
		}
	}
	// Config/script languages whose includes are real file paths must stay flagged.
	for _, lang := range []string{"cpp", "lua", "bash", "fish", "hyprlang", "generic", ""} {
		if ModuleImportLang(lang) {
			t.Errorf("ModuleImportLang(%q) = true, want false", lang)
		}
	}
}
