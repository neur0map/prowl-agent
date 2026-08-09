package docs

import (
	"encoding/json"
	"os"
	"sort"
)

// Source records one ingested documentation set. URL is set for crawled sites;
// Path is set for local ingests.
type Source struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	URL         string `json:"url,omitempty"`
	Path        string `json:"path,omitempty"`
	AddedAt     string `json:"added_at"`
	Pages       int    `json:"pages"`
	Quarantined int    `json:"quarantined,omitempty"`
}

// Manifest is the list of ingested sources in the global docs cache.
type Manifest struct {
	Sources []Source `json:"sources"`
}

// LoadManifest reads the manifest, returning an empty one when absent.
func LoadManifest(home string) (*Manifest, error) {
	data, err := os.ReadFile(manifestPath(home))
	if os.IsNotExist(err) {
		return &Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Save writes the manifest atomically.
func (m *Manifest) Save(home string) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	sort.Slice(m.Sources, func(i, j int) bool { return m.Sources[i].Name < m.Sources[j].Name })
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := manifestPath(home) + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, manifestPath(home))
}

// Upsert replaces any source with the same name, else appends.
func (m *Manifest) Upsert(s Source) {
	for i := range m.Sources {
		if m.Sources[i].Name == s.Name {
			m.Sources[i] = s
			return
		}
	}
	m.Sources = append(m.Sources, s)
}

// Remove drops the named source, reporting whether it existed.
func (m *Manifest) Remove(name string) (Source, bool) {
	for i := range m.Sources {
		if m.Sources[i].Name == name {
			removed := m.Sources[i]
			m.Sources = append(m.Sources[:i], m.Sources[i+1:]...)
			return removed, true
		}
	}
	return Source{}, false
}

// Find returns the named source.
func (m *Manifest) Find(name string) (Source, bool) {
	for _, s := range m.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return Source{}, false
}
