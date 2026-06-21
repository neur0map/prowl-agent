// Package graph resolves the raw edges produced by extractors into a connected
// graph (include trees, exec/keybind chains, resource references) and infers
// per-file roles. Resolution is global and idempotent: it re-resolves the whole
// edge set each run, so incremental re-parsing of changed files stays correct.
package graph

import (
	"path"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// Resolve clears prior resolution and re-links every edge it can.
func Resolve(s *store.Store) error {
	if err := s.ResetResolution(); err != nil {
		return err
	}
	files, err := s.AllFiles()
	if err != nil {
		return err
	}
	fileMap := make(map[string]int64, len(files))
	byID := make(map[int64]store.File, len(files))
	rels := make([]string, 0, len(files))
	for _, f := range files {
		fileMap[f.RelPath] = f.ID
		byID[f.ID] = f
		rels = append(rels, f.RelPath)
	}
	byBase := make(map[string][]int64, len(files))
	for _, f := range files {
		byBase[path.Base(f.RelPath)] = append(byBase[path.Base(f.RelPath)], f.ID)
	}
	qmlStem := make(map[string][]store.File)
	for _, f := range files {
		if f.Lang == "qml" {
			b := path.Base(f.RelPath)
			stem := b[:len(b)-len(path.Ext(b))]
			qmlStem[stem] = append(qmlStem[stem], f)
		}
	}

	// Pass 1: include/import/require/source edges -> files.
	inc, err := s.UnresolvedEdges("includes", "references")
	if err != nil {
		return err
	}
	for _, e := range inc {
		if id, ok := resolvePath(fileMap, rels, e.File, e.Raw, byID[e.FileID].Lang); ok && id != e.FileID {
			if err := s.SetEdgeResolved(e.ID, "file", id); err != nil {
				return err
			}
		}
	}

	// Pass 2: exec/autostart/keybind command strings -> script files.
	ex, err := s.UnresolvedEdges("execs", "binds", "autostarts")
	if err != nil {
		return err
	}
	for _, e := range ex {
		if id, ok := resolveCommandTarget(fileMap, rels, byBase, e.File, e.Raw); ok && id != e.FileID {
			if err := s.SetEdgeResolved(e.ID, "file", id); err != nil {
				return err
			}
		}
	}

	// Pass 3: resource usages/declarations -> resource declarations.
	res, err := s.AllResources()
	if err != nil {
		return err
	}
	decls := make(map[string]int64)
	for _, r := range res {
		if r.Name != "" {
			if _, ok := decls[r.Name]; !ok {
				decls[r.Name] = r.ID
			}
		}
	}
	ruse, err := s.UnresolvedEdges("uses_resource", "declares_resource")
	if err != nil {
		return err
	}
	for _, e := range ruse {
		if id, ok := decls[e.Raw]; ok {
			if err := s.SetEdgeResolved(e.ID, "resource", id); err != nil {
				return err
			}
		}
	}
	// Pass 4: QML component instantiation -> the .qml file defining that component
	// (component name == file stem). Built-in/external types do not match and are
	// dropped afterward so they do not pollute the dangling set.
	inst, err := s.UnresolvedEdges("instantiates")
	if err != nil {
		return err
	}
	for _, e := range inst {
		if id, ok := resolveQMLComponent(qmlStem, byID[e.FileID].RelPath, e.Raw); ok && id != e.FileID {
			if err := s.SetEdgeResolved(e.ID, "file", id); err != nil {
				return err
			}
		}
	}
	if err := s.DeleteUnresolvedEdges("instantiates"); err != nil {
		return err
	}
	// Pass 5: Go package imports -> the files of the imported in-module package.
	if err := resolveGoPackages(s, files, byID); err != nil {
		return err
	}
	// Pass 6: C# `using` imports -> the files declaring the imported namespace.
	if err := resolveCSharpNamespaces(s, byID); err != nil {
		return err
	}
	// Pass 7: Java imports -> the file for that class under any module's source root.
	if err := resolveJavaPackages(s, files, byID); err != nil {
		return err
	}
	return nil
}

// resolveJavaPackages resolves Java imports across a multi-module project. A
// java file's package-relative path (after src/main/java, src/test/java, or
// src/) is its key, so `import com.foo.Bar` resolves to <module>/.../com/foo/
// Bar.java in any module, and `import com.foo.*` fans out to every file in that
// package. Third-party and JDK imports match nothing and stay informational.
func resolveJavaPackages(s *store.Store, files []store.File, byID map[int64]store.File) error {
	pkgFiles := map[string]int64{} // com/foo/Bar.java -> id
	pkgDir := map[string][]int64{} // com/foo -> ids (for wildcard imports)
	for _, f := range files {
		if f.Lang != "java" {
			continue
		}
		rel := javaPkgPath(f.RelPath)
		if rel == "" {
			continue
		}
		pkgFiles[rel] = f.ID
		pkgDir[path.Dir(rel)] = append(pkgDir[path.Dir(rel)], f.ID)
	}
	if len(pkgFiles) == 0 {
		return nil
	}
	inc, err := s.UnresolvedEdges("includes")
	if err != nil {
		return err
	}
	var pkgEdges []store.PkgEdge
	for _, e := range inc {
		if byID[e.FileID].Lang != "java" {
			continue
		}
		if pkg, ok := strings.CutSuffix(e.Raw, ".*"); ok {
			dir := strings.ReplaceAll(pkg, ".", "/")
			for _, dst := range pkgDir[dir] {
				if dst != e.FileID {
					pkgEdges = append(pkgEdges, store.PkgEdge{FileID: e.FileID, DstFileID: dst, Line: e.Line, Raw: e.Raw})
				}
			}
			continue
		}
		key := strings.ReplaceAll(e.Raw, ".", "/") + ".java"
		if dst, ok := pkgFiles[key]; ok && dst != e.FileID {
			if err := s.SetEdgeResolved(e.ID, "file", dst); err != nil {
				return err
			}
		}
	}
	return s.AddPackageEdges(pkgEdges)
}

// javaPkgPath returns a java file's path relative to its source root (the part
// after src/main/java, src/test/java, src/main/kotlin, or src/), i.e. its
// package directory plus filename. Files with no recognizable root keep their
// full path.
func javaPkgPath(rel string) string {
	for _, m := range []string{"/src/main/java/", "/src/test/java/", "/src/main/kotlin/", "/src/"} {
		if i := strings.Index(rel, m); i >= 0 {
			return rel[i+len(m):]
		}
	}
	if strings.HasPrefix(rel, "src/") {
		return rel[len("src/"):]
	}
	return rel
}

// resolveCSharpNamespaces materializes C# namespace dependencies. A file with a
// `using Foo.Bar;` gets a synthetic resolved "pkg" edge to every file declaring
// `namespace Foo.Bar`, so impact, callers, clusters, and entrypoints work across
// a C# project. Framework and third-party usings match no declared namespace and
// are left as informational unresolved imports.
func resolveCSharpNamespaces(s *store.Store, byID map[int64]store.File) error {
	nsFiles, err := s.NamespaceFiles()
	if err != nil {
		return err
	}
	if len(nsFiles) == 0 {
		return nil
	}
	inc, err := s.UnresolvedEdges("includes")
	if err != nil {
		return err
	}
	var pkgEdges []store.PkgEdge
	for _, e := range inc {
		if byID[e.FileID].Lang != "csharp" {
			continue
		}
		for _, dst := range nsFiles[e.Raw] {
			if dst == e.FileID {
				continue
			}
			pkgEdges = append(pkgEdges, store.PkgEdge{FileID: e.FileID, DstFileID: dst, Line: e.Line, Raw: e.Raw})
		}
	}
	return s.AddPackageEdges(pkgEdges)
}

// resolveGoPackages materializes Go package dependencies. A Go file importing an
// in-module package gets a synthetic resolved "pkg" edge to every file of that
// package, so impact, callers, clusters, and entrypoints work across Go
// packages. Standard-library and external imports do not match the module
// prefix and are left as informational unresolved imports.
func resolveGoPackages(s *store.Store, files []store.File, byID map[int64]store.File) error {
	module, _ := s.GetMeta("go_module")
	if module == "" {
		return nil
	}
	// Index the .go files in each package directory.
	goDirFiles := map[string][]int64{}
	for _, f := range files {
		if f.Lang == "go" {
			goDirFiles[path.Dir(f.RelPath)] = append(goDirFiles[path.Dir(f.RelPath)], f.ID)
		}
	}
	inc, err := s.UnresolvedEdges("includes")
	if err != nil {
		return err
	}
	var pkgEdges []store.PkgEdge
	for _, e := range inc {
		if byID[e.FileID].Lang != "go" {
			continue
		}
		dir, ok := moduleImportDir(module, e.Raw)
		if !ok {
			continue // stdlib or external import
		}
		for _, dst := range goDirFiles[dir] {
			if dst == e.FileID {
				continue
			}
			pkgEdges = append(pkgEdges, store.PkgEdge{FileID: e.FileID, DstFileID: dst, Line: e.Line, Raw: e.Raw})
		}
	}
	return s.AddPackageEdges(pkgEdges)
}

// moduleImportDir maps an in-module Go import path to its repo-relative package
// directory. Returns false for the module root and for out-of-module imports.
func moduleImportDir(module, importPath string) (string, bool) {
	if importPath == module {
		return ".", true
	}
	if rest, ok := strings.CutPrefix(importPath, module+"/"); ok {
		return rest, true
	}
	return "", false
}

// resolveQMLComponent links a QML component instantiation (by type name) to the
// .qml file defining it: same directory first (QML's implicit same-dir import),
// then a repo-unique stem, then the candidate sharing the longest path prefix.
func resolveQMLComponent(qmlStem map[string][]store.File, fromRel, name string) (int64, bool) {
	cands := qmlStem[name]
	switch len(cands) {
	case 0:
		return 0, false
	case 1:
		return cands[0].ID, true
	}
	fromDir := path.Dir(fromRel)
	best, bestScore := cands[0].ID, -1
	for _, c := range cands {
		cdir := path.Dir(c.RelPath)
		score := sharedPrefixLen(cdir, fromDir)
		if cdir == fromDir {
			score = 1 << 30
		}
		if score > bestScore {
			best, bestScore = c.ID, score
		}
	}
	return best, true
}

func sharedPrefixLen(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return n
}

// resolvePath resolves a raw include/reference target to a file id.
func resolvePath(fileMap map[string]int64, rels []string, fromRel, raw, lang string) (int64, bool) {
	raw = strings.Trim(strings.TrimSpace(raw), `"'`)
	if raw == "" {
		return 0, false
	}
	for _, c := range pathCandidates(fromRel, raw, lang) {
		if id, ok := fileMap[c]; ok {
			return id, true
		}
	}
	// Unique-suffix fallback (handles dotfiles repos mapped into ~/.config).
	tail := raw
	for _, p := range []string{"~/.config/", "~/", "./", "/"} {
		tail = strings.TrimPrefix(tail, p)
	}
	if tail == "" {
		return 0, false
	}
	var match int64
	cnt := 0
	for _, rel := range rels {
		if rel == tail || strings.HasSuffix(rel, "/"+tail) {
			match = fileMap[rel]
			cnt++
		}
	}
	if cnt == 1 {
		return match, true
	}
	return 0, false
}

func pathCandidates(fromRel, raw, lang string) []string {
	var c []string
	if lang == "lua" && !strings.Contains(raw, "/") {
		mod := strings.ReplaceAll(raw, ".", "/")
		dir := path.Dir(fromRel)
		c = append(c,
			mod+".lua", mod+"/init.lua",
			"lua/"+mod+".lua", "lua/"+mod+"/init.lua",
			path.Join(dir, mod+".lua"), path.Join(dir, mod, "init.lua"),
			path.Join(dir, "lua", mod+".lua"), path.Join(dir, "lua", mod, "init.lua"),
		)
	}
	// TypeScript/JavaScript relative imports omit the extension and may point at a
	// directory's index file; try the usual resolutions. Package imports (no ./ or
	// ../) are external and stay informational.
	if (lang == "typescript" || lang == "tsx" || lang == "javascript") &&
		(strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../")) {
		joined := path.Clean(path.Join(path.Dir(fromRel), raw))
		for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"} {
			c = append(c, joined+ext, path.Join(joined, "index"+ext))
		}
	}
	// Rust crate-relative imports (`use crate::a::b::c;`) map module path segments
	// to files under the importing file's crate root, longest match first (a
	// submodule a/b/c.rs, else the module a/b.rs that defines item c). The crate
	// root is the enclosing `src/` dir, so this works for both a single crate at
	// the repo root and a Cargo workspace (crates/<name>/src/...). super::/self::
	// and cross-crate imports stay informational.
	if lang == "rust" {
		if rest, ok := strings.CutPrefix(raw, "crate::"); ok {
			if i := strings.Index(rest, "::{"); i >= 0 {
				rest = rest[:i]
			}
			segs := strings.Split(rest, "::")
			srcRoot := "src/"
			if i := strings.Index(fromRel, "/src/"); i >= 0 {
				srcRoot = fromRel[:i+len("/src/")]
			}
			for n := len(segs); n >= 1; n-- {
				p := srcRoot + strings.Join(segs[:n], "/")
				c = append(c, p+".rs", p+"/mod.rs")
			}
			c = append(c, srcRoot+"lib.rs", srcRoot+"main.rs")
		}
		// `mod foo;` includes a sibling module file. In foo/mod.rs, lib.rs or
		// main.rs the submodule is a sibling (dir/foo.rs); in foo.rs it lives in a
		// subdir named after the file (dir/foo/bar.rs). Try both.
		if rest, ok := strings.CutPrefix(raw, "mod::"); ok {
			dir := path.Dir(fromRel)
			c = append(c, path.Join(dir, rest)+".rs", path.Join(dir, rest, "mod.rs"))
			base := strings.TrimSuffix(path.Base(fromRel), ".rs")
			if base != "mod" && base != "lib" && base != "main" {
				c = append(c, path.Join(dir, base, rest)+".rs", path.Join(dir, base, rest, "mod.rs"))
			}
		}
	}
	// Python absolute imports (`import a.b`, `from a.b import c`) map the dotted
	// module to a file: a/b.py or a/b/__init__.py, also under src/ for that
	// layout. Third-party imports stay informational.
	if lang == "python" && raw != "" && !strings.HasPrefix(raw, ".") {
		mod := strings.ReplaceAll(raw, ".", "/")
		c = append(c, mod+".py", mod+"/__init__.py", "src/"+mod+".py", "src/"+mod+"/__init__.py")
	}
	// Python relative imports (`from .foo import x`, `from ..pkg.mod import y`):
	// the leading dots count levels up from the importing file's package, and the
	// remaining dotted path maps to a file there. A bare `from . import x` has no
	// module path and stays informational.
	if lang == "python" && strings.HasPrefix(raw, ".") {
		dots := 0
		for dots < len(raw) && raw[dots] == '.' {
			dots++
		}
		if modpath := strings.ReplaceAll(raw[dots:], ".", "/"); modpath != "" {
			base := path.Dir(fromRel)
			for i := 1; i < dots; i++ {
				base = path.Dir(base)
			}
			target := path.Join(base, modpath)
			c = append(c, target+".py", target+"/__init__.py")
		}
	}
	// Java imports (`import com.foo.Bar;`) map the dotted class path to a file,
	// including the Maven/Gradle src/main/java layout. Wildcard and third-party
	// imports do not match a project file and stay informational.
	if lang == "java" && raw != "" {
		mod := strings.ReplaceAll(raw, ".", "/")
		c = append(c, mod+".java", "src/main/java/"+mod+".java", "src/"+mod+".java")
	}
	// Ruby require/require_relative: require_relative is relative to the requiring
	// file's directory; require resolves from the project root or lib/. Gems do
	// not match a project file and stay informational.
	if lang == "ruby" && raw != "" {
		c = append(c, raw+".rb", "lib/"+raw+".rb",
			path.Clean(path.Join(path.Dir(fromRel), raw))+".rb")
	}
	if strings.HasPrefix(raw, "~/.config/") {
		c = append(c, strings.TrimPrefix(raw, "~/.config/"))
	}
	if strings.HasPrefix(raw, "~/") {
		c = append(c, strings.TrimPrefix(raw, "~/"))
	}
	if !strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "~") {
		c = append(c, path.Clean(path.Join(path.Dir(fromRel), raw)))
	}
	c = append(c, strings.TrimPrefix(raw, "/"))
	return c
}

// resolveCommandTarget resolves a command string to an indexed file: first the
// bare command name against repo command files by basename (e.g. ryoku-pkg-add
// -> bin/ryoku-pkg-add), then any path-like token referring to a script.
func resolveCommandTarget(fileMap map[string]int64, rels []string, byBase map[string][]int64, fromRel, cmd string) (int64, bool) {
	toks := strings.Fields(cmd)
	if len(toks) > 0 {
		first := strings.Trim(toks[0], `"'`)
		if first != "" && !strings.ContainsAny(first, "/$") {
			if ids, ok := byBase[first]; ok && len(ids) == 1 {
				return ids[0], true
			}
		}
	}
	for _, t := range toks {
		t = strings.Trim(t, `"'`)
		if strings.Contains(t, "/") || strings.HasSuffix(t, ".sh") || strings.HasSuffix(t, ".py") || strings.HasSuffix(t, ".lua") {
			if id, ok := resolvePath(fileMap, rels, fromRel, t, ""); ok {
				return id, true
			}
		}
	}
	return 0, false
}
