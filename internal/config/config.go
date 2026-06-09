// Package config loads and saves a project's .prowl/config.toml and rules.toml.
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// AI holds the optional semantic-assist settings (wired in M2).
type AI struct {
	Enabled     bool   `toml:"enabled"`
	EmbedModel  string `toml:"embed_model"`
	RerankModel string `toml:"rerank_model"`
	AssistModel string `toml:"assist_model"`
	OllamaURL   string `toml:"ollama_url"`
}

// Config is the per-project configuration.
type Config struct {
	Languages []string `toml:"languages"`
	Ignore    []string `toml:"ignore"`
	AI        AI       `toml:"ai"`
}

// Rule is a deterministic architecture/health rule consumed by violations/doctor.
type Rule struct {
	Name        string `toml:"name"`
	Kind        string `toml:"kind"`
	Description string `toml:"description"`
}

// Forbid declares a forbidden dependency crossing: any resolved edge whose
// source path matches From and target path matches To is a violation.
type Forbid struct {
	Name string `toml:"name"`
	From string `toml:"from"` // glob on the source file path
	To   string `toml:"to"`   // glob on the target file path
}

// Rules is the set of architecture rules for a project.
type Rules struct {
	Rule   []Rule   `toml:"rule"`
	Forbid []Forbid `toml:"forbid"`
}

const (
	configName = "config.toml"
	rulesName  = "rules.toml"
)

// Default returns the starting configuration for a new workspace.
func Default() Config {
	p := PresetByName(DefaultTier)
	return Config{
		Languages: []string{"lua", "python", "bash", "css", "scss", "json", "yaml", "toml", "ini", "qml", "hyprlang", "rasi", "generic"},
		Ignore:    []string{".mcp.json", "*.log", "*.png", "*.jpg", "*.jpeg", "*.gif", "*.ttf", "*.otf", "*.woff", "*.woff2"},
		AI: AI{
			Enabled:     false,
			EmbedModel:  p.EmbedModel,
			AssistModel: p.AssistModel,
			OllamaURL:   "http://localhost:11434",
		},
	}
}

// ModelPreset is a named pair of local models for the AI layer.
type ModelPreset struct {
	Name        string
	Desc        string
	EmbedModel  string
	AssistModel string
}

// DefaultTier is the recommended preset when none is chosen.
const DefaultTier = "smart"

// Presets are the AI model tiers offered at init, fastest to best. Sizes and
// VRAM are rough guidance for choosing one.
var Presets = []ModelPreset{
	{"fast", "runs anywhere, CPU ok (~1 GB)", "embeddinggemma", "gemma3:1b"},
	{"smart", "best balance, ~8 GB VRAM (~5 GB download)", "qwen3-embedding:4b", "gemma3:4b"},
	{"max", "highest quality, ~16 GB VRAM (~10 GB download)", "qwen3-embedding:8b", "gemma3:4b"},
}

// PresetByName returns the named preset, falling back to the default tier.
func PresetByName(name string) ModelPreset {
	for _, p := range Presets {
		if p.Name == name {
			return p
		}
	}
	for _, p := range Presets {
		if p.Name == DefaultTier {
			return p
		}
	}
	return Presets[0]
}

// DefaultRules returns the starter rule set for a project.
func DefaultRules() Rules {
	return Rules{Rule: []Rule{
		{Name: "no-dangling-includes", Kind: "dangling_includes", Description: "every source/include/import/require must resolve to a file in the project"},
		{Name: "no-orphan-scripts", Kind: "orphan_script", Description: "scripts should be referenced by some config or keybind"},
		{Name: "use-theme-variables", Kind: "hardcoded_color", Description: "prefer theme variables over hardcoded color literals"},
	}}
}

// Load reads config.toml from dir, returning Default() if absent.
func Load(dir string) (Config, error) {
	c := Default()
	p := filepath.Join(dir, configName)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return c, nil
	}
	_, err := toml.DecodeFile(p, &c)
	return c, err
}

// Save writes config.toml into dir.
func Save(dir string, c Config) error {
	return encode(filepath.Join(dir, configName), c)
}

// LoadRules reads rules.toml from dir, returning DefaultRules() if absent.
func LoadRules(dir string) (Rules, error) {
	r := DefaultRules()
	p := filepath.Join(dir, rulesName)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return r, nil
	}
	r = Rules{}
	_, err := toml.DecodeFile(p, &r)
	return r, err
}

// SaveRules writes rules.toml into dir.
func SaveRules(dir string, r Rules) error {
	return encode(filepath.Join(dir, rulesName), r)
}

func encode(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(v)
}
