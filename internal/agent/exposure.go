package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/profile"
)

const exposureSchemaVersion = "prowl.exposure/v1"

var ErrInvalidExposureManifest = errors.New("invalid exposure manifest")

// SourceExposure records authority and identity without source or secret bodies.
type SourceExposure struct {
	ID              string             `json:"id"`
	Kind            profile.SourceKind `json:"kind"`
	Hash            string             `json:"hash"`
	Provenance      profile.Provenance `json:"provenance"`
	Scope           profile.Scope      `json:"scope"`
	Precedence      profile.Precedence `json:"precedence"`
	Trust           profile.Trust      `json:"trust"`
	SecretReference string             `json:"secret_reference,omitempty"`
	Reason          string             `json:"reason"`
}

// ToolExposure records identity and schema hash for an exposed tool.
type ToolExposure struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

// SkillExposure records discoverable skill metadata for the exposure manifest.
type SkillExposure struct {
	ID           string `json:"id"`
	ContentHash  string `json:"content_hash"`
	Root         string `json:"root"`
	ForcePreload bool   `json:"force_preload,omitempty"`
}

// PreloadedBodyExposure records an explicitly force-preloaded skill body reference.
type PreloadedBodyExposure struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

type exposureWire struct {
	SchemaVersion        string                  `json:"schema_version"`
	SnapshotID           string                  `json:"snapshot_id"`
	Core                 profile.CoreRecord      `json:"core"`
	Included             []SourceExposure        `json:"included"`
	Omitted              []SourceExposure        `json:"omitted"`
	ToolSchemaGeneration string                  `json:"tool_schema_generation"`
	ToolSchemas          []ToolExposure          `json:"tool_schemas"`
	ToolSetHash          string                  `json:"tool_set_hash"`
	Skills               []SkillExposure         `json:"skills"`
	PreloadedSkillBodies []PreloadedBodyExposure `json:"preloaded_skill_bodies"`
	SecretReferences     []string                `json:"secret_references"`
	PromptHash           string                  `json:"prompt_hash"`
}

// ExposureManifest is an opaque immutable view of frozen prompt exposure.
type ExposureManifest struct {
	wire      exposureWire
	canonical []byte
	id        string
}

func NewExposureManifest(snapshot *profile.Snapshot) (*ExposureManifest, error) {
	if snapshot == nil {
		return nil, ErrInvalidExposureManifest
	}
	// Compute prompt hash before building exposure.
	promptBytes, err := PromptBytes(snapshot)
	if err != nil {
		return nil, ErrInvalidExposureManifest
	}
	promptDigest := sha256.Sum256(promptBytes)
	promptHash := hex.EncodeToString(promptDigest[:])

	coreRec := snapshot.CoreRecord()
	wire := exposureWire{
		SchemaVersion:        exposureSchemaVersion,
		SnapshotID:           snapshot.ID(),
		Core:                 coreRec,
		Included:             []SourceExposure{},
		Omitted:              []SourceExposure{},
		ToolSchemaGeneration: snapshot.ToolSchemaGeneration(),
		ToolSchemas:          []ToolExposure{},
		Skills:               []SkillExposure{},
		PreloadedSkillBodies: []PreloadedBodyExposure{},
		SecretReferences:     []string{},
		PromptHash:           promptHash,
	}

	profileRecord := snapshot.ProfileRecord()
	policy := snapshot.Policy()

	wire.Included = append(wire.Included,
		SourceExposure{
			ID: "policy:active", Kind: profile.PermissionPolicySource, Hash: policy.Hash,
			Provenance: policy.Provenance, Scope: policy.Scope, Precedence: policy.Precedence,
			Trust: policy.Trust, Reason: "active permission and approval policy",
		},
		SourceExposure{
			ID: "profile:local", Kind: profile.ProfilePolicySource, Hash: profileRecord.Hash,
			Provenance: profileRecord.Provenance, Scope: profileRecord.Scope, Precedence: profileRecord.Precedence,
			Trust: profileRecord.Trust, Reason: "active profile identity and surface policy",
		},
	)

	for _, source := range snapshot.Sources() {
		exposure := SourceExposure{
			ID: source.ID, Kind: source.Kind, Hash: source.Hash, Provenance: source.Provenance,
			Scope: source.Scope, Precedence: source.Precedence, Trust: source.Trust,
			SecretReference: source.SecretReference, Reason: source.Reason,
		}
		if source.Included {
			wire.Included = append(wire.Included, exposure)
		} else {
			wire.Omitted = append(wire.Omitted, exposure)
		}
		if source.SecretReference != "" {
			wire.SecretReferences = append(wire.SecretReferences, source.SecretReference)
		}
	}

	// Build tool exposure with schema hashes; compute tool set hash over sorted IDs.
	tools := snapshot.Tools()
	var toolSetBuf []byte
	for _, tool := range tools {
		wire.ToolSchemas = append(wire.ToolSchemas, ToolExposure{ID: tool.ID, Hash: tool.Hash})
		toolSetBuf = append(toolSetBuf, tool.ID...)
		toolSetBuf = append(toolSetBuf, '\x00')
		toolSetBuf = append(toolSetBuf, tool.Hash...)
		toolSetBuf = append(toolSetBuf, '\x00')
	}
	toolSetDigest := sha256.Sum256(toolSetBuf)
	wire.ToolSetHash = hex.EncodeToString(toolSetDigest[:])

	for _, skill := range snapshot.Skills() {
		wire.Skills = append(wire.Skills, SkillExposure{
			ID:           skill.ID,
			ContentHash:  skill.ContentHash,
			Root:         skill.Root,
			ForcePreload: skill.ForcePreload,
		})
	}

	for _, body := range snapshot.PreloadedSkills() {
		wire.PreloadedSkillBodies = append(wire.PreloadedSkillBodies, PreloadedBodyExposure{
			ID:   body.ID,
			Hash: body.Hash,
		})
	}

	sort.Slice(wire.Included, func(left, right int) bool {
		a, b := wire.Included[left], wire.Included[right]
		if a.Precedence.Strength() != b.Precedence.Strength() {
			return a.Precedence.Strength() < b.Precedence.Strength()
		}
		return a.ID < b.ID
	})
	sort.Slice(wire.Omitted, func(left, right int) bool {
		a, b := wire.Omitted[left], wire.Omitted[right]
		if a.Precedence.Strength() != b.Precedence.Strength() {
			return a.Precedence.Strength() < b.Precedence.Strength()
		}
		return a.ID < b.ID
	})
	sort.Strings(wire.SecretReferences)
	// ToolSchemas and Skills are already in snapshot order (sorted by ID).
	return freezeExposure(wire)
}

// OpenExposureManifest validates and reopens exact canonical persisted bytes.
func OpenExposureManifest(canonical []byte) (*ExposureManifest, error) {
	var wire exposureWire
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, ErrInvalidExposureManifest
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidExposureManifest
	}
	if err := validateExposure(wire); err != nil {
		return nil, err
	}
	reopened, err := freezeExposure(wire)
	if err != nil || !bytes.Equal(reopened.canonical, canonical) {
		return nil, ErrInvalidExposureManifest
	}
	return reopened, nil
}

func (manifest *ExposureManifest) ID() string             { return manifest.id }
func (manifest *ExposureManifest) SnapshotID() string     { return manifest.wire.SnapshotID }
func (manifest *ExposureManifest) CanonicalBytes() []byte { return bytes.Clone(manifest.canonical) }
func (manifest *ExposureManifest) Included() []SourceExposure {
	return slices.Clone(manifest.wire.Included)
}
func (manifest *ExposureManifest) Omitted() []SourceExposure {
	return slices.Clone(manifest.wire.Omitted)
}
func (manifest *ExposureManifest) ToolSchemas() []ToolExposure {
	return slices.Clone(manifest.wire.ToolSchemas)
}
func (manifest *ExposureManifest) ToolSetHash() string     { return manifest.wire.ToolSetHash }
func (manifest *ExposureManifest) Skills() []SkillExposure { return slices.Clone(manifest.wire.Skills) }
func (manifest *ExposureManifest) PreloadedSkillBodies() []PreloadedBodyExposure {
	return slices.Clone(manifest.wire.PreloadedSkillBodies)
}
func (manifest *ExposureManifest) SecretReferences() []string {
	return slices.Clone(manifest.wire.SecretReferences)
}
func (manifest *ExposureManifest) PromptHash() string { return manifest.wire.PromptHash }

func freezeExposure(wire exposureWire) (*ExposureManifest, error) {
	if err := validateExposure(wire); err != nil {
		return nil, err
	}
	canonical, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		return nil, ErrInvalidExposureManifest
	}
	canonical = append(canonical, '\n')
	digest := sha256.Sum256(canonical)
	return &ExposureManifest{wire: cloneExposureWire(wire), canonical: canonical, id: hex.EncodeToString(digest[:])}, nil
}

func validateExposure(wire exposureWire) error {
	if wire.SchemaVersion != exposureSchemaVersion || !validHash(wire.SnapshotID) ||
		strings.TrimSpace(wire.ToolSchemaGeneration) == "" ||
		!validHash(wire.ToolSetHash) || !validHash(wire.PromptHash) {
		return ErrInvalidExposureManifest
	}
	// Validate core record.
	expected, ok := profile.CoreRecordForVersion(wire.Core.Version)
	if !ok || wire.Core != expected {
		return ErrInvalidExposureManifest
	}
	// Validate sorted uniqueness of list fields.
	if !sortedUniqueStrings(wire.SecretReferences) {
		return ErrInvalidExposureManifest
	}
	if !sortedToolExposures(wire.ToolSchemas) || !sortedSkillExposures(wire.Skills) || !sortedPreloadedBodies(wire.PreloadedSkillBodies) {
		return ErrInvalidExposureManifest
	}
	// Validate included and omitted source exposures.
	all := append(slices.Clone(wire.Included), wire.Omitted...)
	for index, source := range all {
		precedence, trust, ok := profile.Authority(source.Kind)
		if !ok || precedence != source.Precedence || trust != source.Trust || !profile.ValidSourceClassification(source.Kind, source.Provenance, source.Scope) || strings.TrimSpace(source.ID) == "" || strings.TrimSpace(source.Reason) == "" || !validHash(source.Hash) {
			return ErrInvalidExposureManifest
		}
		if source.Kind == profile.SecretReferenceSource {
			if strings.TrimSpace(source.SecretReference) == "" {
				return ErrInvalidExposureManifest
			}
		} else if source.SecretReference != "" {
			return ErrInvalidExposureManifest
		}
		for previous := range index {
			if all[previous].ID == source.ID {
				return ErrInvalidExposureManifest
			}
		}
	}
	if !sortedExposure(wire.Included) || !sortedExposure(wire.Omitted) {
		return ErrInvalidExposureManifest
	}
	// Validate tool schema hashes and recompute ToolSetHash for cross-validation.
	var toolSetBuf []byte
	for _, tool := range wire.ToolSchemas {
		if strings.TrimSpace(tool.ID) == "" || !validHash(tool.Hash) {
			return ErrInvalidExposureManifest
		}
		toolSetBuf = append(toolSetBuf, tool.ID...)
		toolSetBuf = append(toolSetBuf, '\x00')
		toolSetBuf = append(toolSetBuf, tool.Hash...)
		toolSetBuf = append(toolSetBuf, '\x00')
	}
	expectedToolSetDigest := sha256.Sum256(toolSetBuf)
	if hex.EncodeToString(expectedToolSetDigest[:]) != wire.ToolSetHash {
		return ErrInvalidExposureManifest
	}
	// Validate skill exposures.
	for _, skill := range wire.Skills {
		if strings.TrimSpace(skill.ID) == "" || !validHash(skill.ContentHash) || strings.TrimSpace(skill.Root) == "" {
			return ErrInvalidExposureManifest
		}
	}
	// Validate preloaded skill body hashes.
	for _, body := range wire.PreloadedSkillBodies {
		if strings.TrimSpace(body.ID) == "" || !validHash(body.Hash) {
			return ErrInvalidExposureManifest
		}
	}
	return nil
}

func cloneExposureWire(wire exposureWire) exposureWire {
	wire.Included = slices.Clone(wire.Included)
	wire.Omitted = slices.Clone(wire.Omitted)
	wire.ToolSchemas = slices.Clone(wire.ToolSchemas)
	wire.Skills = slices.Clone(wire.Skills)
	wire.PreloadedSkillBodies = slices.Clone(wire.PreloadedSkillBodies)
	wire.SecretReferences = slices.Clone(wire.SecretReferences)
	return wire
}

func sortedExposure(items []SourceExposure) bool {
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

func sortedToolExposures(items []ToolExposure) bool {
	for index, item := range items {
		if strings.TrimSpace(item.ID) == "" || (index > 0 && items[index-1].ID >= item.ID) {
			return false
		}
	}
	return true
}

func sortedSkillExposures(items []SkillExposure) bool {
	for index, item := range items {
		if strings.TrimSpace(item.ID) == "" || (index > 0 && items[index-1].ID >= item.ID) {
			return false
		}
	}
	return true
}

func sortedPreloadedBodies(items []PreloadedBodyExposure) bool {
	for index, item := range items {
		if strings.TrimSpace(item.ID) == "" || (index > 0 && items[index-1].ID >= item.ID) {
			return false
		}
	}
	return true
}

func sortedUniqueStrings(items []string) bool {
	for index, item := range items {
		if strings.TrimSpace(item) == "" || (index > 0 && items[index-1] >= item) {
			return false
		}
	}
	return true
}

func validHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
