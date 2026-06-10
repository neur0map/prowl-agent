package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// GlobalConfig is the binary-wide preference, remembered across projects so the
// AI/tier choice is set once. It lives outside any project so a fresh `init`
// inherits the last choice instead of re-prompting, and a re-init never has to
// guess. It is distinct from the project list in the workspace registry.
type GlobalConfig struct {
	AIEnabled   bool   `toml:"ai_enabled"`
	Tier        string `toml:"tier"`
	EmbedModel  string `toml:"embed_model"`
	AssistModel string `toml:"assist_model"`
}

// globalPath returns $XDG_CONFIG_HOME/prowl-agent/config.toml (or the
// ~/.config fallback).
func globalPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "prowl-agent", "config.toml"), nil
}

// LoadGlobal reads the global config, returning a zero-value GlobalConfig when
// the file is absent (a first run).
func LoadGlobal() (GlobalConfig, error) {
	var g GlobalConfig
	p, err := globalPath()
	if err != nil {
		return g, err
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return g, nil
	}
	_, err = toml.DecodeFile(p, &g)
	return g, err
}

// SaveGlobal writes the global config, creating parent directories.
func SaveGlobal(g GlobalConfig) error {
	p, err := globalPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return encode(p, g)
}

// GlobalExists reports whether a global config has been written (so init knows
// whether a remembered preference exists, distinct from a zero-value default).
func GlobalExists() bool {
	p, err := globalPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}
