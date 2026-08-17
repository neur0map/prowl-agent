package skills

import (
	"strings"
	"testing"
)

// A skill's directory name is its installed identity: setup writes each skill to
// <client>/skills/<dir>/SKILL.md, and agents match the frontmatter name. If the
// two disagree the skill installs under one name and announces another, so
// nothing reliably resolves it.
func TestEachSkillNameMatchesItsDirectory(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("no skills embedded")
	}
	for _, skill := range all {
		declared := frontmatterValue(skill.Content, "name:")
		if declared != skill.Name {
			t.Errorf("skill %s declares name %q; the two must match", skill.Name, declared)
		}
	}
}

// The description is the only thing an agent reads when deciding whether to
// reach for a skill at all, so an empty or truncated one silently disables it.
// Length bounds are the convention agent harnesses share: long enough to list
// real triggering symptoms, short enough to stay in a tool listing.
func TestEachSkillDescribesWhenToUseIt(t *testing.T) {
	for _, skill := range All() {
		description := Description(skill.Content)
		switch {
		case description == "":
			t.Errorf("skill %s has no description; agents cannot route to it", skill.Name)
		case len(description) < 80:
			t.Errorf("skill %s description is %d chars, too short to state when it applies: %q",
				skill.Name, len(description), description)
		case len(description) > 700:
			t.Errorf("skill %s description is %d chars; trim it to stay readable in a skill listing",
				skill.Name, len(description))
		}
		if !strings.Contains(strings.ToLower(description), "use ") {
			t.Errorf("skill %s description does not state a triggering condition (\"Use when ...\"): %q",
				skill.Name, description)
		}
	}
}

// frontmatterValue reads one scalar key from the leading YAML frontmatter.
func frontmatterValue(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" && strings.HasPrefix(content, "---") {
			continue
		}
		if strings.HasPrefix(trimmed, key) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, key))
		}
	}
	return ""
}
