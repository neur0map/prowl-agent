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

// The CLI-first cutover replaces prowl-repo-exploration with a single canonical
// code-search skill. The active registry must expose code-search and must never
// surface the retired skill or the legacy ownership tree, which exists only so
// setup can recognize and remove old installed copies.
func TestCodeSearchIsTheOnlyActiveSearchSkill(t *testing.T) {
	names := Names()
	if !hasName(names, "code-search") {
		t.Errorf("All() must expose code-search; got %v", names)
	}
	if hasName(names, "prowl-repo-exploration") {
		t.Errorf("retired skill prowl-repo-exploration is still active: %v", names)
	}
	if hasName(names, "legacy") {
		t.Errorf("the legacy ownership tree leaked into All(): %v", names)
	}
}

// The description is selected from metadata alone, so it must name the tasks that
// should route to Prowl: repository/code search, symbols, callers, structure,
// architecture, and change impact.
func TestCodeSearchDescriptionNamesItsTriggers(t *testing.T) {
	skill, ok := findSkill("code-search")
	if !ok {
		t.Fatal("code-search skill is not embedded")
	}
	if name := frontmatterValue(skill.Content, "name:"); name != "code-search" {
		t.Errorf("code-search frontmatter name = %q, want it to match the directory", name)
	}
	description := strings.ToLower(Description(skill.Content))
	for _, trigger := range []string{"search", "symbol", "caller", "structure", "architecture", "impact"} {
		if !strings.Contains(description, trigger) {
			t.Errorf("code-search description omits the %q trigger: %q", trigger, description)
		}
	}
	if !strings.Contains(description, "repositor") && !strings.Contains(description, "code search") {
		t.Errorf("code-search description does not name repository/code search: %q", description)
	}
}

// The body routes structural work through the prowl-agent CLI and never presents
// MCP as a transport choice: an agent should not have to pick between CLI and
// MCP, and the retired MCP tool names must be gone.
func TestCodeSearchBodyRoutesThroughTheCLINotMCP(t *testing.T) {
	skill, ok := findSkill("code-search")
	if !ok {
		t.Fatal("code-search skill is not embedded")
	}
	if !strings.Contains(skill.Content, "prowl-agent ") {
		t.Error("code-search body invokes no prowl-agent CLI command")
	}
	lower := strings.ToLower(skill.Content)
	if strings.Contains(lower, "mcp") {
		t.Error("code-search body still presents MCP as a transport choice")
	}
	for _, banned := range []string{"search_context", "read_symbol", "mcp__"} {
		if strings.Contains(lower, banned) {
			t.Errorf("code-search body still names the MCP tool %q", banned)
		}
	}
}

// The routing boundary must lead the skill body, before any command catalog, so
// a model reads it first: grep is for exact literal/regex text, glob is for
// filename patterns, and everything structural goes to Prowl.
func TestCodeSearchOpensWithTheGrepGlobBoundary(t *testing.T) {
	skill, ok := findSkill("code-search")
	if !ok {
		t.Fatal("code-search skill is not embedded")
	}
	opening := strings.ToLower(openingSection(skill.Content))
	if !strings.Contains(opening, "grep") || (!strings.Contains(opening, "literal") && !strings.Contains(opening, "regex")) {
		t.Errorf("opening does not reserve grep for exact literal/regex work: %q", opening)
	}
	if !strings.Contains(opening, "glob") || !strings.Contains(opening, "filename") {
		t.Errorf("opening does not reserve glob for filename patterns: %q", opening)
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

// hasName reports whether names contains want.
func hasName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// findSkill returns the embedded active skill with the given name.
func findSkill(name string) (Skill, bool) {
	for _, skill := range All() {
		if skill.Name == name {
			return skill, true
		}
	}
	return Skill{}, false
}

// openingSection returns the skill body before its first level-2 heading -- the
// title and intro a model reads before any command catalog. Frontmatter is
// stripped so the boundary is asserted against real body prose, not metadata.
func openingSection(content string) string {
	body := content
	if strings.HasPrefix(body, "---\n") {
		if end := strings.Index(body[4:], "\n---"); end >= 0 {
			body = body[4+end+len("\n---"):]
		}
	}
	if i := strings.Index(body, "\n## "); i >= 0 {
		return body[:i]
	}
	return body
}
