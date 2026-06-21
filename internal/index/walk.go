// Package index walks a project, hashes files, drives Tree-sitter extraction, and
// keeps the SQLite graph incrementally up to date.
package index

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cespare/xxhash/v2"
)

// alwaysSkipDirs are never walked.
var alwaysSkipDirs = map[string]bool{
	".git": true, ".prowl": true, "node_modules": true,
	".cursor": true, ".vscode": true, ".zed": true, ".idea": true, ".helix": true,
	".omp": true, ".factory": true,
}

// walkFiles invokes fn for each non-ignored file under root, honoring .gitignore
// and extra ignore globs and always skipping .prowl/, .git/, node_modules/.
func walkFiles(root string, ignore []string, fn func(rel string, d fs.DirEntry) error) error {
	patterns := append(loadGitignore(root), ignore...)
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
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
		return fn(rel, d)
	})
}

// Walk returns rel paths under root, honoring .gitignore and extra ignore globs,
// and always skipping .prowl/, .git/, node_modules/.
func Walk(root string, ignore []string) ([]string, error) {
	var out []string
	err := walkFiles(root, ignore, func(rel string, _ fs.DirEntry) error {
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out, err
}

// Signature is a content-free fingerprint of the project's tracked files: a hash
// over each file's path and modification time. It changes when any file is
// added, removed, renamed, or edited, so the CLI can skip the expensive
// read-and-hash re-index when nothing changed. It only reads directory entries
// and stats, never file contents, so it stays fast on large repositories.
func Signature(root string, ignore []string) (uint64, error) {
	var entries []string
	err := walkFiles(root, ignore, func(rel string, d fs.DirEntry) error {
		var mt int64
		if info, ierr := d.Info(); ierr == nil {
			mt = info.ModTime().UnixNano()
		}
		entries = append(entries, rel+"\x00"+strconv.FormatInt(mt, 10))
		return nil
	})
	if err != nil {
		return 0, err
	}
	sort.Strings(entries)
	h := xxhash.New()
	for _, e := range entries {
		_, _ = h.WriteString(e)
		_, _ = h.WriteString("\n")
	}
	return h.Sum64(), nil
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
