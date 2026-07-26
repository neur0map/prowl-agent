package agent

import (
	"encoding/json"
	"errors"

	"github.com/prowl-agent/prowl-agent/internal/profile"
)

const promptSchemaVersion = "prowl.prompt/v1"

var ErrInvalidPromptSnapshot = errors.New("invalid prompt snapshot")

type promptWire struct {
	SchemaVersion        string                  `json:"schema_version"`
	SnapshotID           string                  `json:"snapshot_id"`
	CorePromptVersion    string                  `json:"core_prompt_version"`
	Provider             profile.ProviderModel   `json:"provider"`
	PrincipalID          string                  `json:"principal_id"`
	Profile              profile.ProfileRecord   `json:"profile"`
	Policy               profile.Policy          `json:"policy"`
	Sources              []profile.Source         `json:"sources"`
	ToolSchemaGeneration string                  `json:"tool_schema_generation"`
	Tools                 []profile.ToolSchema    `json:"tools"`
	Skills                []profile.SkillMetadata `json:"skills"`
	PreloadedSkills       []profile.SkillBody     `json:"preloaded_skills"`
}

// PromptBytes returns the byte-stable frozen provider-independent prefix.
func PromptBytes(snapshot *profile.Snapshot) ([]byte, error) {
	if snapshot == nil {
		return nil, ErrInvalidPromptSnapshot
	}
	allSources := snapshot.Sources()
	included := make([]profile.Source, 0, len(allSources))
	for _, source := range allSources {
		if source.Included {
			included = append(included, source)
		}
	}
	wire := promptWire{
		SchemaVersion: promptSchemaVersion,
		SnapshotID: snapshot.ID(),
		CorePromptVersion: snapshot.CorePromptVersion(),
		Provider: snapshot.Provider(),
		PrincipalID: snapshot.PrincipalID(),
		Profile: snapshot.ProfileRecord(),
		Policy: snapshot.Policy(),
		Sources: included,
		ToolSchemaGeneration: snapshot.ToolSchemaGeneration(),
		Tools: snapshot.Tools(),
		Skills: snapshot.Skills(),
		PreloadedSkills: snapshot.PreloadedSkills(),
	}
	canonical, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		return nil, ErrInvalidPromptSnapshot
	}
	return append(canonical, '\n'), nil
}
