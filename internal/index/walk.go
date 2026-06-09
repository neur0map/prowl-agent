// Package index walks a project, hashes files, drives Tree-sitter extraction, and
// keeps the SQLite graph incrementally up to date.
package index

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// alwaysSkipDirs are never walked.
var alwaysSkipDirs = map[string]bool{
	".git": true, ".prowl": true, "node_modules": true,
	".cursor": true, ".vscode": true, ".zed": true, ".idea": true, ".helix": true,
}

// Walk returns rel paths under root, honoring .gitignore and extra ignore globs,
// and always skipping .prowl/, .git/, node_modules/.
func Walk(root string, ignore []string) ([]string, error) {
	patterns := append(loadGitignore(root), ignore...)
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if alwaysSkipDirs[d.Name()] || matchAny(patterns, rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if matchAny(patterns, rel, false) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out, err
}

func loadGitignore(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return nil
	}
	var pats []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pats = append(pats, line)
	}
	return pats
}

// matchAny reports whether rel matches any gitignore-style pattern. It supports
// a pragmatic subset: basename globs, full-path globs, and bare directory names.
func matchAny(pats []string, rel string, isDir bool) bool {
	base := filepath.Base(rel)
	segs := strings.Split(rel, "/")
	for _, p := range pats {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") || strings.HasPrefix(p, "!") {
			continue
		}
		p = strings.TrimSuffix(strings.TrimPrefix(p, "/"), "/")
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
		for _, s := range segs {
			if s == p {
				return true
			}
		}
	}
	return false
}
