// Package skills embeds the agent skills prowl-agent installs into a project so
// coding agents learn when to reach for prowl. Each skill is a SKILL.md with
// YAML frontmatter (name, description) whose description states the triggering
// conditions, following the convention agents use to decide when a skill applies.
package skills

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

//go:embed */SKILL.md
var files embed.FS

// Skill is one installable agent skill: a directory name and its SKILL.md body.
type Skill struct {
	Name    string
	Content string
}

// All returns every embedded skill, sorted by name.
func All() []Skill {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil
	}
	out := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := files.ReadFile(entry.Name() + "/SKILL.md")
		if err != nil {
			continue
		}
		out = append(out, Skill{Name: entry.Name(), Content: string(data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names returns the skill names, sorted.
func Names() []string {
	skills := All()
	names := make([]string, len(skills))
	for i, skill := range skills {
		names[i] = skill.Name
	}
	return names
}

// Description extracts the frontmatter description line from a SKILL.md body,
// for callers that want to preview a skill without shipping the whole file.
func Description(content string) string {
	const key = "description:"
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, key))
		}
	}
	return ""
}
