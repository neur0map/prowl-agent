// Package docs ingests external documentation (crawled sites or local trees)
// into a shared, indexed Markdown corpus that prowl retrieves with the same
// budget-bounded, cited, deterministic engine it uses for code. The corpus is
// global (one cache per machine) because documentation for a library is the
// same across every project that depends on it.
package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Home returns the global docs cache root, honoring XDG_DATA_HOME and falling
// back to ~/.local/share. Callers may override the whole path with
// PROWL_DOCS_HOME (used by tests).
func Home() (string, error) {
	if override := os.Getenv("PROWL_DOCS_HOME"); override != "" {
		return override, nil
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "prowl-agent", "docs"), nil
}

func storePath(home string) string     { return filepath.Join(home, "index.db") }
func sourcesDir(home string) string    { return filepath.Join(home, "sources") }
func quarantineDir(home string) string { return filepath.Join(home, "quarantine") }
func manifestPath(home string) string  { return filepath.Join(home, "manifest.json") }

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slug derives a stable, filesystem-safe directory name from a source name or
// URL, so each source's crawled pages live under sources/<slug>/.
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "source"
	}
	if len(s) > 64 {
		s = strings.Trim(s[:64], "-")
	}
	return s
}
