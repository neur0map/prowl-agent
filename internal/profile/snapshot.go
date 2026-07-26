package profile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
)

const snapshotSchemaVersion = "prowl.profile-snapshot/v1"

var ErrInvalidSnapshot = errors.New("invalid profile snapshot")

type ProviderModel struct {
	ProviderID      string `json:"provider_id"`
	ModelID         string `json:"model_id"`
	MaxInputTokens  int    `json:"max_input_tokens"`
	MaxOutputTokens int    `json:"max_output_tokens"`
}

type PolicyInput struct {
	Permission string
	Approval   string
	Readiness  string
}

type SourceInput struct {
	ID              string
	Kind            SourceKind
	Body            string
	SecretReference string
	Provenance      Provenance
	Scope           Scope
	Included        bool
	Reason          string
}

type ToolSchemaInput struct {
	ID     string
	Schema []byte
}

type SkillMetadataInput struct {
	ID          string
	Name        string
	Description string
}

type SkillBodyInput struct {
	ID   string
	Body string
}

type SnapshotInput struct {
	Provider             ProviderModel
	CorePromptVersion    string
	PrincipalID          string
	Profile              Profile
	Policy               PolicyInput
	ToolSchemaGeneration string
	Sources               []SourceInput
	Tools                 []ToolSchemaInput
	Skills                []SkillMetadataInput
	PreloadedSkills       []SkillBodyInput
}

type ProfileRecord struct {
	Identity      Identity   `json:"identity"`
	Soul          string     `json:"soul"`
	SurfacePolicy string     `json:"surface_policy"`
	Hash          string     `json:"hash"`
	Provenance    Provenance `json:"provenance"`
	Scope         Scope      `json:"scope"`
	Precedence    Precedence `json:"precedence"`
	Trust         Trust      `json:"trust"`
}

type Policy struct {
	Permission string     `json:"permission"`
	Approval   string     `json:"approval"`
	Readiness  string     `json:"readiness"`
	Hash       string     `json:"hash"`
	Provenance Provenance `json:"provenance"`
	Scope      Scope      `json:"scope"`
	Precedence Precedence `json:"precedence"`
	Trust      Trust      `json:"trust"`
}
type Source struct {
	ID              string     `json:"id"`
	Kind            SourceKind `json:"kind"`
	Body            string     `json:"body,omitempty"`
	Hash            string     `json:"hash"`
	Provenance      Provenance `json:"provenance"`
	Scope           Scope      `json:"scope"`
	Precedence      Precedence `json:"precedence"`
	Trust           Trust      `json:"trust"`
	SecretReference string     `json:"secret_reference,omitempty"`
	Included        bool       `json:"included"`
	Reason          string     `json:"reason"`
}

type ToolSchema struct {
	ID     string          `json:"id"`
	Schema json.RawMessage `json:"schema"`
	Hash   string          `json:"hash"`
}

type SkillMetadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Hash        string `json:"hash"`
}

type SkillBody struct {
	ID   string `json:"id"`
	Body string `json:"body"`
	Hash string `json:"hash"`
}

type snapshotWire struct {
	SchemaVersion        string          `json:"schema_version"`
	Provider             ProviderModel   `json:"provider"`
	CorePromptVersion    string          `json:"core_prompt_version"`
	PrincipalID          string          `json:"principal_id"`
	Profile              ProfileRecord   `json:"profile"`
	Policy               Policy          `json:"policy"`
	ToolSchemaGeneration string          `json:"tool_schema_generation"`
	Sources               []Source        `json:"sources"`
	Tools                 []ToolSchema    `json:"tools"`
	Skills                []SkillMetadata `json:"skills"`
	PreloadedSkills       []SkillBody     `json:"preloaded_skills"`
}

// Snapshot is an opaque immutable session-start snapshot.
type Snapshot struct {
	wire      snapshotWire
	canonical []byte
	id        string
}

func NewSnapshot(input SnapshotInput) (*Snapshot, error) {
	if !input.Profile.valid() || !validProvider(input.Provider) || !nonempty(input.CorePromptVersion) || !nonempty(input.PrincipalID) || !nonempty(input.ToolSchemaGeneration) {
		return nil, ErrInvalidSnapshot
	}
	if !nonempty(input.Policy.Permission) || !nonempty(input.Policy.Approval) || !nonempty(input.Policy.Readiness) {
		return nil, ErrInvalidSnapshot
	}
	wire := snapshotWire{
		SchemaVersion: snapshotSchemaVersion,
		Provider: input.Provider,
		CorePromptVersion: input.CorePromptVersion,
		PrincipalID: input.PrincipalID,
		Profile: ProfileRecord{
			Identity: input.Profile.Identity(), Soul: input.Profile.Soul(), SurfacePolicy: input.Profile.SurfacePolicy(),
			Hash: hashParts(string(input.Profile.Identity()), input.Profile.Soul(), input.Profile.SurfacePolicy()),
			Provenance: BuiltinProvenance, Scope: ProfileScope, Precedence: ProfilePrecedence, Trust: ProfileTrust,
		},
		Policy: Policy{
			Permission: input.Policy.Permission, Approval: input.Policy.Approval, Readiness: input.Policy.Readiness,
			Hash: hashParts(input.Policy.Permission, input.Policy.Approval, input.Policy.Readiness),
			Provenance: BuiltinProvenance, Scope: SessionScope, Precedence: ExecutableSystemPrecedence, Trust: ExecutableTrust,
		},
		ToolSchemaGeneration: input.ToolSchemaGeneration,
		Sources: make([]Source, 0, len(input.Sources)),
		Tools: make([]ToolSchema, 0, len(input.Tools)),
		Skills: make([]SkillMetadata, 0, len(input.Skills)),
		PreloadedSkills: make([]SkillBody, 0, len(input.PreloadedSkills)),
	}
	for _, item := range input.Sources {
		source, err := buildSource(item)
		if err != nil {
			return nil, err
		}
		wire.Sources = append(wire.Sources, source)
	}
	for _, item := range input.Tools {
		if !nonempty(item.ID) {
			return nil, ErrInvalidSnapshot
		}
		schema, err := canonicalSchema(item.Schema)
		if err != nil {
			return nil, err
		}
		wire.Tools = append(wire.Tools, ToolSchema{ID: item.ID, Schema: schema, Hash: hashBytes(schema)})
	}
	for _, item := range input.Skills {
		if !nonempty(item.ID) || !nonempty(item.Name) || !nonempty(item.Description) {
			return nil, ErrInvalidSnapshot
		}
		wire.Skills = append(wire.Skills, SkillMetadata{ID: item.ID, Name: item.Name, Description: item.Description, Hash: hashParts(item.ID, item.Name, item.Description)})
	}
	for _, item := range input.PreloadedSkills {
		if !nonempty(item.ID) || !nonempty(item.Body) {
			return nil, ErrInvalidSnapshot
		}
		wire.PreloadedSkills = append(wire.PreloadedSkills, SkillBody{ID: item.ID, Body: item.Body, Hash: hashBytes([]byte(item.Body))})
	}
	sortWire(&wire)
	if err := validateWire(wire); err != nil {
		return nil, err
	}
	return freezeSnapshot(wire)
}

// OpenSnapshot validates and reopens exact canonical persisted bytes without
// resolving any mutable profile, tool, skill, or project input.
func OpenSnapshot(canonical []byte) (*Snapshot, error) {
	var wire snapshotWire
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, ErrInvalidSnapshot
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, ErrInvalidSnapshot
	}
	if err := normalizeToolSchemas(&wire); err != nil {
		return nil, err
	}
	if err := validateWire(wire); err != nil {
		return nil, err
	}
	reopened, err := freezeSnapshot(wire)
	if err != nil || !bytes.Equal(reopened.canonical, canonical) {
		return nil, ErrInvalidSnapshot
	}
	return reopened, nil
}

func (snapshot *Snapshot) ID() string { return snapshot.id }
func (snapshot *Snapshot) CanonicalBytes() []byte { return bytes.Clone(snapshot.canonical) }
func (snapshot *Snapshot) Provider() ProviderModel { return snapshot.wire.Provider }
func (snapshot *Snapshot) CorePromptVersion() string { return snapshot.wire.CorePromptVersion }
func (snapshot *Snapshot) PrincipalID() string { return snapshot.wire.PrincipalID }
func (snapshot *Snapshot) Profile() Profile { return Local() }
func (snapshot *Snapshot) ProfileRecord() ProfileRecord { return snapshot.wire.Profile }
func (snapshot *Snapshot) Policy() Policy { return snapshot.wire.Policy }
func (snapshot *Snapshot) ToolSchemaGeneration() string { return snapshot.wire.ToolSchemaGeneration }
func (snapshot *Snapshot) Sources() []Source { return slices.Clone(snapshot.wire.Sources) }
func (snapshot *Snapshot) Skills() []SkillMetadata { return slices.Clone(snapshot.wire.Skills) }
func (snapshot *Snapshot) PreloadedSkills() []SkillBody { return slices.Clone(snapshot.wire.PreloadedSkills) }

func (snapshot *Snapshot) Tools() []ToolSchema {
	tools := make([]ToolSchema, len(snapshot.wire.Tools))
	for index, tool := range snapshot.wire.Tools {
		tools[index] = tool
		tools[index].Schema = bytes.Clone(tool.Schema)
	}
	return tools
}

func buildSource(input SourceInput) (Source, error) {
	precedence, trust, ok := Authority(input.Kind)
	if !ok || !nonempty(input.ID) || !ValidSourceClassification(input.Kind, input.Provenance, input.Scope) || !nonempty(input.Reason) {
		return Source{}, ErrInvalidSnapshot
	}
	source := Source{
		ID: input.ID, Kind: input.Kind, Provenance: input.Provenance, Scope: input.Scope,
		Precedence: precedence, Trust: trust, Included: input.Included, Reason: input.Reason,
	}
	if input.Kind == SecretReferenceSource {
		if input.Body != "" || !nonempty(input.SecretReference) {
			return Source{}, ErrInvalidSnapshot
		}
		source.SecretReference = input.SecretReference
		source.Hash = hashParts("secret-reference", input.SecretReference)
		return source, nil
	}
	if input.SecretReference != "" || !nonempty(input.Body) {
		return Source{}, ErrInvalidSnapshot
	}
	source.Hash = hashBytes([]byte(input.Body))
	if input.Included {
		source.Body = input.Body
	}
	return source, nil
}

func sortWire(wire *snapshotWire) {
	sort.Slice(wire.Sources, func(left, right int) bool {
		a, b := wire.Sources[left], wire.Sources[right]
		if a.Precedence.Strength() != b.Precedence.Strength() {
			return a.Precedence.Strength() < b.Precedence.Strength()
		}
		return a.ID < b.ID
	})
	sort.Slice(wire.Tools, func(left, right int) bool { return wire.Tools[left].ID < wire.Tools[right].ID })
	sort.Slice(wire.Skills, func(left, right int) bool { return wire.Skills[left].ID < wire.Skills[right].ID })
	sort.Slice(wire.PreloadedSkills, func(left, right int) bool { return wire.PreloadedSkills[left].ID < wire.PreloadedSkills[right].ID })
}

func validateWire(wire snapshotWire) error {
	if wire.SchemaVersion != snapshotSchemaVersion || !validProvider(wire.Provider) || !nonempty(wire.CorePromptVersion) || !nonempty(wire.PrincipalID) || !nonempty(wire.ToolSchemaGeneration) {
		return ErrInvalidSnapshot
	}
	local := Local()
	if wire.Profile.Identity != LocalIdentity || wire.Profile.Soul != local.Soul() || wire.Profile.SurfacePolicy != local.SurfacePolicy() || wire.Profile.Hash != hashParts(string(LocalIdentity), local.Soul(), local.SurfacePolicy()) || wire.Profile.Provenance != BuiltinProvenance || wire.Profile.Scope != ProfileScope || wire.Profile.Precedence != ProfilePrecedence || wire.Profile.Trust != ProfileTrust {
		return ErrInvalidSnapshot
	}
	if !nonempty(wire.Policy.Permission) || !nonempty(wire.Policy.Approval) || !nonempty(wire.Policy.Readiness) || wire.Policy.Hash != hashParts(wire.Policy.Permission, wire.Policy.Approval, wire.Policy.Readiness) || wire.Policy.Provenance != BuiltinProvenance || wire.Policy.Scope != SessionScope || wire.Policy.Precedence != ExecutableSystemPrecedence || wire.Policy.Trust != ExecutableTrust {
		return ErrInvalidSnapshot
	}
	if !sortedUniqueSources(wire.Sources) || !sortedUniqueTools(wire.Tools) || !sortedUniqueSkills(wire.Skills) || !sortedUniqueBodies(wire.PreloadedSkills) {
		return ErrInvalidSnapshot
	}
	for _, source := range wire.Sources {
		precedence, trust, ok := Authority(source.Kind)
		if !ok || precedence != source.Precedence || trust != source.Trust || !ValidSourceClassification(source.Kind, source.Provenance, source.Scope) || !nonempty(source.ID) || !nonempty(source.Reason) {
			return ErrInvalidSnapshot
		}
		if source.Kind == SecretReferenceSource {
			if source.Body != "" || !nonempty(source.SecretReference) || source.Hash != hashParts("secret-reference", source.SecretReference) {
				return ErrInvalidSnapshot
			}
		} else if source.SecretReference != "" || !validHash(source.Hash) || (source.Included && (!nonempty(source.Body) || source.Hash != hashBytes([]byte(source.Body)))) || (!source.Included && source.Body != "") {
			return ErrInvalidSnapshot
		}
	}
	skillIDs := make([]string, len(wire.Skills))
	for index, skill := range wire.Skills {
		if !nonempty(skill.ID) || !nonempty(skill.Name) || !nonempty(skill.Description) || skill.Hash != hashParts(skill.ID, skill.Name, skill.Description) {
			return ErrInvalidSnapshot
		}
		skillIDs[index] = skill.ID
	}
	for _, body := range wire.PreloadedSkills {
		if !nonempty(body.ID) || !nonempty(body.Body) || body.Hash != hashBytes([]byte(body.Body)) || !slices.Contains(skillIDs, body.ID) {
			return ErrInvalidSnapshot
		}
	}
	for _, tool := range wire.Tools {
		canonical, err := canonicalSchema(tool.Schema)
		if err != nil || tool.Hash != hashBytes(canonical) {
			return ErrInvalidSnapshot
		}
	}
	return nil
}

func normalizeToolSchemas(wire *snapshotWire) error {
	for index := range wire.Tools {
		canonical, err := canonicalSchema(wire.Tools[index].Schema)
		if err != nil {
			return err
		}
		wire.Tools[index].Schema = canonical
	}
	return nil
}

func freezeSnapshot(wire snapshotWire) (*Snapshot, error) {
	canonical, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}
	canonical = append(canonical, '\n')
	return &Snapshot{wire: cloneWire(wire), canonical: canonical, id: hashBytes(canonical)}, nil
}

func cloneWire(wire snapshotWire) snapshotWire {
	wire.Sources = slices.Clone(wire.Sources)
	wire.Skills = slices.Clone(wire.Skills)
	wire.PreloadedSkills = slices.Clone(wire.PreloadedSkills)
	tools := make([]ToolSchema, len(wire.Tools))
	for index, tool := range wire.Tools {
		tools[index] = tool
		tools[index].Schema = bytes.Clone(tool.Schema)
	}
	wire.Tools = tools
	return wire
}

func canonicalSchema(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, ErrInvalidSnapshot
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, ErrInvalidSnapshot
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, ErrInvalidSnapshot
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalidSnapshot
	}
	return canonical, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalidSnapshot
	}
	return nil
}

func validProvider(provider ProviderModel) bool {
	return nonempty(provider.ProviderID) && nonempty(provider.ModelID) && provider.MaxInputTokens > 0 && provider.MaxOutputTokens > 0
}

func hashParts(parts ...string) string {
	buffer := bytes.NewBuffer(nil)
	for index, part := range parts {
		if index > 0 {
			buffer.WriteByte('\n')
		}
		buffer.WriteString(part)
	}
	return hashBytes(buffer.Bytes())
}

func hashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func validHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func sortedUniqueSources(items []Source) bool {
	for index, item := range items {
		if index > 0 {
			previous := items[index-1]
			if previous.Precedence.Strength() > item.Precedence.Strength() || (previous.Precedence == item.Precedence && previous.ID >= item.ID) {
				return false
			}
		}
	}
	return true
}

func sortedUniqueTools(items []ToolSchema) bool {
	for index, item := range items {
		if index > 0 && items[index-1].ID >= item.ID {
			return false
		}
	}
	return true
}

func sortedUniqueSkills(items []SkillMetadata) bool {
	for index, item := range items {
		if index > 0 && items[index-1].ID >= item.ID {
			return false
		}
	}
	return true
}

func sortedUniqueBodies(items []SkillBody) bool {
	for index, item := range items {
		if index > 0 && items[index-1].ID >= item.ID {
			return false
		}
	}
	return true
}
