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

// docDampening scales prose documentation (Markdown and friends). Docs describe
// code rather than implement it, and match natural-language questions densely,
// so for a code question they should rank below the code; but they are more
// relevant than pure noise, so the factor is milder than lowSignalDampening.
const docDampening = 0.5

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
	case isCIPath(p, base):
		return "ci"
	case isDocPath(base):
		return "docs"
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

// isCIPath reports whether a path is CI or repository-meta configuration
// (GitHub and other providers) rather than project code. It answers CI
// questions but is noise for ordinary code queries, so it is down-weighted
// unless the query names CI.
func isCIPath(p, base string) bool {
	if strings.HasPrefix(p, ".github/") || strings.Contains(p, "/.github/") {
		return true
	}
	if strings.HasPrefix(p, ".circleci/") || strings.Contains(p, "/.circleci/") {
		return true
	}
	switch base {
	case ".gitlab-ci.yml", ".travis.yml", "azure-pipelines.yml", "appveyor.yml", "jenkinsfile", ".pre-commit-config.yaml":
		return true
	}
	return false
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

// isDocPath reports whether a path is prose documentation (Markdown and
// friends), which describes code rather than implementing it. Knowledge
// documents are separate candidates and are never classified here.
func isDocPath(base string) bool {
	switch filepath.Ext(base) {
	case ".md", ".mdx", ".markdown", ".rst", ".adoc":
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
	case "ci":
		keys = []string{"ci", "cicd", "workflow", "workflows", "action", "actions", "pipeline", "pipelines", "release", "deploy", "deployment"}
	case "docs":
		keys = []string{"docs", "doc", "documentation", "readme", "changelog", "guide", "guides", "tutorial", "howto"}
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
