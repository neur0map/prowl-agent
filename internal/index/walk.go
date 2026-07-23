// Package index walks a project, hashes files, drives Tree-sitter extraction, and
// keeps the SQLite graph incrementally up to date.
package index

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cespare/xxhash/v2"
)

// alwaysSkipDirs are never walked.
var alwaysSkipDirs = map[string]bool{
	".git": true, ".prowl": true, "node_modules": true,
	".cursor": true, ".vscode": true, ".zed": true, ".idea": true, ".helix": true,
	".omp": true, ".factory": true,
}

// walkFilesContext invokes fn for each non-ignored file under root, honoring .gitignore
// (the root one and every nested one, each scoped to its own directory) and
// extra ignore globs, and always skipping .prowl/, .git/, node_modules/.
func walkFilesContext(ctx context.Context, root string, ignore []string, fn func(rel string, d fs.DirEntry) error) error {
	gitignores := map[string][]string{"": loadGitignore(root)} // rel dir -> patterns
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
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
			if alwaysSkipDirs[d.Name()] || ignoredBy(gitignores, ignore, rel, true) {
				return filepath.SkipDir
			}
			if pats := loadGitignore(p); len(pats) > 0 {
				gitignores[rel] = pats // applies to this directory's subtree
			}
			return nil
		}
		if ignoredBy(gitignores, ignore, rel, false) {
			return nil
		}
		return fn(rel, d)
	})
}

// ignoredBy reports whether rel is ignored, composing the root and every
// ancestor-directory .gitignore (each matched against the path relative to its
// own directory, shallowest first so a deeper directory's rule wins) and finally
// the extra ignore globs. A directory's own .gitignore never ignores itself.
func ignoredBy(gitignores map[string][]string, ignore []string, rel string, isDir bool) bool {
	ignored := false
	for _, a := range ancestorDirs(rel) {
		pats, ok := gitignores[a]
		if !ok {
			continue
		}
		sub := rel
		if a != "" {
			sub = rel[len(a)+1:]
		}
		if v, m := matchAny(pats, sub, isDir); m {
			ignored = v
		}
	}
	if v, m := matchAny(ignore, rel, isDir); m {
		ignored = v
	}
	return ignored
}

// ancestorDirs returns rel's ancestor directories from the root ("") down to its
// parent: the directories whose .gitignore could apply to rel.
func ancestorDirs(rel string) []string {
	dirs := []string{""}
	parts := strings.Split(rel, "/")
	for i := 0; i < len(parts)-1; i++ {
		dirs = append(dirs, strings.Join(parts[:i+1], "/"))
	}
	return dirs
}

// Walk returns rel paths under root, honoring .gitignore and extra ignore globs,
// and always skipping .prowl/, .git/, node_modules/.
func Walk(root string, ignore []string) ([]string, error) {
	return WalkContext(context.Background(), root, ignore)
}

// WalkContext is Walk with cancellation checks during directory traversal.
func WalkContext(ctx context.Context, root string, ignore []string) ([]string, error) {
	var out []string
	err := walkFilesContext(ctx, root, ignore, func(rel string, _ fs.DirEntry) error {
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out, err
}

// Signature fingerprints the indexing policy and every tracked file's path,
// size, modification time, and content. Content closes the same-size/same-mtime
// replacement hole; the streamed read remains context-cancellable.
func Signature(root string, ignore []string) (uint64, error) {
	return SignatureWithOptions(root, Options{Ignore: ignore})
}

// SignatureWithOptions includes indexing policy in the freshness fingerprint so
// changing configured languages invalidates an otherwise unchanged file tree.
func SignatureWithOptions(root string, opt Options) (uint64, error) {
	return SignatureWithOptionsContext(context.Background(), root, opt)
}

// SignatureWithOptionsContext is SignatureWithOptions with cancellation checks
// during traversal and fingerprint construction.
func SignatureWithOptionsContext(ctx context.Context, root string, opt Options) (uint64, error) {
	snapshot, err := SourceSnapshotWithOptionsContext(ctx, root, opt)
	return snapshot.Signature, err
}

// SourceSnapshot binds a freshness signature to the exact path set used to
// construct it. Publication validation uses Paths to reject an index walk that
// transiently omitted a source present in the accepted pre-snapshot.
type SourceSnapshot struct {
	Signature uint64
	Paths     []string
}

// SourceSnapshotWithOptionsContext constructs a content-aware source snapshot.
func SourceSnapshotWithOptionsContext(ctx context.Context, root string, opt Options) (SourceSnapshot, error) {
	var entries []string
	var paths []string
	err := walkFilesContext(ctx, root, opt.Ignore, func(rel string, d fs.DirEntry) error {
		info, err := d.Info()
		if err != nil {
			return err
		}
		content, err := fileSignature(ctx, filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		entries = append(entries, fmt.Sprintf("%s\x00%d\x00%d\x00%x", rel, info.Size(), info.ModTime().UnixNano(), content))
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return SourceSnapshot{}, err
	}
	sort.Strings(entries)
	h := xxhash.New()
	languages := append([]string(nil), opt.Languages...)
	for i := range languages {
		languages[i] = strings.ToLower(strings.TrimSpace(languages[i]))
	}
	if len(languages) == 0 {
		languages = []string{"auto"}
	}
	sort.Strings(languages)
	_, _ = h.WriteString("languages\x00" + strings.Join(languages, ",") + "\n")
	for _, pattern := range opt.Ignore {
		_, _ = h.WriteString("ignore\x00" + pattern + "\n")
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return SourceSnapshot{}, err
		}
		_, _ = h.WriteString(e)
		_, _ = h.WriteString("\n")
	}
	sort.Strings(paths)
	return SourceSnapshot{Signature: h.Sum64(), Paths: paths}, nil
}

func fileSignature(ctx context.Context, path string) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	h := xxhash.New()
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			_, _ = h.Write(buffer[:n])
		}
		if readErr == io.EOF {
			return h.Sum64(), nil
		}
		if readErr != nil {
			return 0, readErr
		}
	}
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

// matchAny reports whether rel is ignored by the gitignore-style patterns and
// whether any pattern matched at all (so callers can compose multiple gitignore
// files, deeper-wins). Order matters: a later matching pattern overrides an
// earlier one, and a leading "!" re-includes. A trailing "/" restricts a pattern
// to directories. The supported subset is basename globs, anchored path globs
// (no "**"), and bare directory names; negation lets a repo ignore a tree but
// keep a subtree (e.g. `packages/*/*/` then `!packages/*/src/`).
func matchAny(pats []string, rel string, isDir bool) (ignored, matched bool) {
	base := filepath.Base(rel)
	segs := strings.Split(rel, "/")
	for _, p := range pats {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		negate := strings.HasPrefix(p, "!")
		if negate {
			p = p[1:]
		}
		dirOnly := strings.HasSuffix(p, "/")
		p = strings.TrimSuffix(strings.TrimPrefix(p, "/"), "/")
		if p == "" || (dirOnly && !isDir) {
			continue
		}
		if matchPattern(p, base, rel, segs) {
			ignored, matched = !negate, true
		}
	}
	return
}

// matchPattern reports whether a single gitignore pattern matches rel. A pattern
// with no slash is a basename glob matching at any depth (or a bare directory
// name matching any path segment); a pattern with a slash is anchored to the
// project root and matched against the whole path.
func matchPattern(p, base, rel string, segs []string) bool {
	if !strings.Contains(p, "/") {
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
		for _, s := range segs {
			if s == p {
				return true
			}
		}
		return false
	}
	ok, _ := filepath.Match(p, rel)
	return ok
}
