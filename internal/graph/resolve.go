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

	// Pass 1: include/import/require/source edges -> files.
	inc, err := s.UnresolvedEdges("includes", "references")
	if err != nil {
		return err
	}
	for _, e := range inc {
		lua := byID[e.FileID].Lang == "lua"
		if id, ok := resolvePath(fileMap, rels, e.File, e.Raw, lua); ok && id != e.FileID {
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
	return nil
}

// resolvePath resolves a raw include/reference target to a file id.
func resolvePath(fileMap map[string]int64, rels []string, fromRel, raw string, lua bool) (int64, bool) {
	raw = strings.Trim(strings.TrimSpace(raw), `"'`)
	if raw == "" {
		return 0, false
	}
	for _, c := range pathCandidates(fromRel, raw, lua) {
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

func pathCandidates(fromRel, raw string, lua bool) []string {
	var c []string
	if lua && !strings.Contains(raw, "/") {
		mod := strings.ReplaceAll(raw, ".", "/")
		dir := path.Dir(fromRel)
		c = append(c,
			mod+".lua", mod+"/init.lua",
			"lua/"+mod+".lua", "lua/"+mod+"/init.lua",
			path.Join(dir, mod+".lua"), path.Join(dir, mod, "init.lua"),
			path.Join(dir, "lua", mod+".lua"), path.Join(dir, "lua", mod, "init.lua"),
		)
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
			if id, ok := resolvePath(fileMap, rels, fromRel, t, false); ok {
				return id, true
			}
		}
	}
	return 0, false
}
