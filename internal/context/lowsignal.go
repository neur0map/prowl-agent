package context

import (
	"path/filepath"
	"strings"
)

// lowSignalDampening scales the lexical score of a low-signal candidate. Low-
// signal files (locale tables, generated code, lockfiles, minified bundles)
// often contain dense keyword matches that are coincidental rather than the
// code an agent is looking for, so their lexical evidence is worth less. A
// direct identifier match or a query that names the class overrides this.
const lowSignalDampening = 0.35

// lowSignalClass classifies a project-relative path into a low-signal file
// class, or returns "" for ordinary source. Classification is by path and
// filename convention only, so it is deterministic and language-agnostic.
func lowSignalClass(path string) string {
	p := strings.ToLower(filepath.ToSlash(path))
	base := p
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		base = p[idx+1:]
	}
	switch {
	case isLockfilePath(base):
		return "lockfile"
	case isMinifiedPath(base):
		return "minified"
	case isGeneratedPath(p, base):
		return "generated"
	case isLocalePath(p, base):
		return "locale"
	}
	return ""
}

func isLockfilePath(base string) bool {
	switch base {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "npm-shrinkwrap.json",
		"go.sum", "cargo.lock", "gemfile.lock", "poetry.lock", "composer.lock",
		"pipfile.lock", "flake.lock", "bun.lockb", "packages.lock.json",
		"pubspec.lock", "mix.lock", "gradle.lockfile":
		return true
	}
	return strings.HasSuffix(base, ".lock")
}

func isMinifiedPath(base string) bool {
	return strings.HasSuffix(base, ".min.js") ||
		strings.HasSuffix(base, ".min.css") ||
		strings.HasSuffix(base, ".min.mjs") ||
		strings.HasSuffix(base, ".map")
}

func isGeneratedPath(p, base string) bool {
	if strings.HasPrefix(p, "generated/") || strings.Contains(p, "/generated/") {
		return true
	}
	return strings.HasSuffix(base, ".pb.go") ||
		strings.HasSuffix(base, ".pb.cc") ||
		strings.HasSuffix(base, "_pb2.py") ||
		strings.HasSuffix(base, ".gen.go") ||
		strings.HasSuffix(base, "_gen.go") ||
		strings.HasSuffix(base, "_generated.go") ||
		strings.HasSuffix(base, ".g.dart") ||
		strings.HasSuffix(base, ".freezed.dart")
}

func isLocalePath(p, base string) bool {
	for _, dir := range []string{"locale", "locales", "i18n", "l10n", "translations", "lang", "langs"} {
		if strings.HasPrefix(p, dir+"/") || strings.Contains(p, "/"+dir+"/") {
			return true
		}
	}
	switch filepath.Ext(base) {
	case ".po", ".pot", ".arb", ".xliff", ".xlf":
		return true
	}
	return false
}

// queryWantsClass reports whether the query itself names a low-signal class, in
// which case those files are legitimate answers and must not be down-weighted.
func queryWantsClass(terms []string, class string) bool {
	var keys []string
	switch class {
	case "lockfile":
		keys = []string{"lock", "lockfile", "lockfiles", "dependency", "dependencies", "pinned"}
	case "locale":
		keys = []string{"locale", "locales", "i18n", "l10n", "translation", "translations", "translate", "translated", "language", "languages", "lang"}
	case "generated":
		keys = []string{"generated", "generate", "codegen", "protobuf", "proto", "grpc", "schema"}
	case "minified":
		keys = []string{"minified", "minify", "bundle", "bundled", "sourcemap"}
	}
	for _, term := range terms {
		for _, key := range keys {
			if term == key {
				return true
			}
		}
	}
	return false
}
