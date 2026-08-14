package capability

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed builtin/*.yaml
var builtinFS embed.FS

// Catalog stores validated capability manifests.
type Catalog struct{ manifests map[string]Manifest }

func BuiltinCatalog() (*Catalog, error) {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil, err
	}
	catalog := &Catalog{manifests: map[string]Manifest{}}
	for _, entry := range entries {
		data, err := builtinFS.ReadFile("builtin/" + entry.Name())
		if err != nil {
			return nil, err
		}
		var manifest Manifest
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if manifest.Name == "" || manifest.Title == "" || manifest.Description == "" || manifest.Version == "" {
			return nil, fmt.Errorf("%s: missing required capability fields", entry.Name())
		}
		if _, duplicate := catalog.manifests[manifest.Name]; duplicate {
			return nil, fmt.Errorf("duplicate capability %q", manifest.Name)
		}
		catalog.manifests[manifest.Name] = manifest
	}
	return catalog, nil
}

func (catalog *Catalog) Get(name string) (Manifest, bool) {
	manifest, ok := catalog.manifests[name]
	return manifest, ok
}

// All returns every manifest, name-sorted, for callers that rank the catalog
// themselves (e.g. semantic capability search over the bundled embedder).
func (catalog *Catalog) All() []Manifest {
	out := make([]Manifest, 0, len(catalog.manifests))
	for _, m := range catalog.manifests {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (catalog *Catalog) Search(query string, limit int) []Summary {
	if limit <= 0 {
		limit = 10
	}
	terms := strings.Fields(strings.ToLower(query))
	type scored struct {
		manifest Manifest
		score    int
	}
	var matches []scored
	for _, manifest := range catalog.manifests {
		score := capabilityScore(manifest, terms)
		if len(terms) > 0 && score == 0 {
			continue
		}
		matches = append(matches, scored{manifest, score})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].manifest.Name < matches[j].manifest.Name
		}
		return matches[i].score > matches[j].score
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]Summary, len(matches))
	for index, match := range matches {
		out[index] = match.manifest.Summary()
	}
	return out
}

func capabilityScore(manifest Manifest, terms []string) int {
	nameTitle := strings.ToLower(manifest.Name + " " + manifest.Title)
	description := strings.ToLower(manifest.Description)
	triggers := strings.ToLower(strings.Join(manifest.Triggers, " "))
	outputs := strings.ToLower(strings.Join(manifest.Outputs, " "))
	score := 0
	for _, term := range terms {
		term = strings.Trim(term, ".,:;!?()[]{}\"'")
		if strings.Contains(nameTitle, term) {
			score += 5
		}
		if strings.Contains(triggers, term) {
			score += 4
		}
		if strings.Contains(description, term) {
			score += 2
		}
		if strings.Contains(outputs, term) {
			score++
		}
	}
	return score
}
