package capability

// Manifest describes a discoverable workflow without embedding its full prompt
// or tool schemas in every agent turn.
type Manifest struct {
	Name        string   `yaml:"name" json:"name"`
	Title       string   `yaml:"title" json:"title"`
	Description string   `yaml:"description" json:"description"`
	Triggers    []string `yaml:"triggers" json:"triggers"`
	Requires    []string `yaml:"requires" json:"requires"`
	Outputs     []string `yaml:"outputs" json:"outputs"`
	Privacy     string   `yaml:"privacy" json:"privacy"`
	ReadOnly    bool     `yaml:"read_only" json:"read_only"`
	Version     string   `yaml:"version" json:"version"`
	Prompts     []string `yaml:"prompts" json:"prompts"`
	Resources   []string `yaml:"resources" json:"resources"`
	Tools       []string `yaml:"tools" json:"tools"`
}

// Summary is the token-lean discovery projection.
type Summary struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Privacy     string   `json:"privacy"`
	Version     string   `json:"version"`
	ReadOnly    bool     `json:"read_only"`
	Triggers    []string `json:"triggers"`
}
