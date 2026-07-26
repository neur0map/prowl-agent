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

type exposureWire struct {
	SchemaVersion          string           `json:"schema_version"`
	SnapshotID             string           `json:"snapshot_id"`
	Included               []SourceExposure `json:"included"`
	Omitted                []SourceExposure `json:"omitted"`
	ToolSchemaGeneration   string           `json:"tool_schema_generation"`
	ToolSchemaIDs          []string         `json:"tool_schema_ids"`
	SkillMetadataIDs       []string         `json:"skill_metadata_ids"`
	PreloadedSkillBodyIDs  []string         `json:"preloaded_skill_body_ids"`
	SecretReferences       []string         `json:"secret_references"`
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
	wire := exposureWire{
		SchemaVersion: exposureSchemaVersion,
		SnapshotID: snapshot.ID(),
		Included: []SourceExposure{},
		Omitted: []SourceExposure{},
		ToolSchemaGeneration: snapshot.ToolSchemaGeneration(),
		ToolSchemaIDs: []string{},
		SkillMetadataIDs: []string{},
		PreloadedSkillBodyIDs: []string{},
		SecretReferences: []string{},
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
	for _, tool := range snapshot.Tools() {
		wire.ToolSchemaIDs = append(wire.ToolSchemaIDs, tool.ID)
	}
	for _, skill := range snapshot.Skills() {
		wire.SkillMetadataIDs = append(wire.SkillMetadataIDs, skill.ID)
	}
	for _, skill := range snapshot.PreloadedSkills() {
		wire.PreloadedSkillBodyIDs = append(wire.PreloadedSkillBodyIDs, skill.ID)
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

func (manifest *ExposureManifest) ID() string { return manifest.id }
func (manifest *ExposureManifest) SnapshotID() string { return manifest.wire.SnapshotID }
func (manifest *ExposureManifest) CanonicalBytes() []byte { return bytes.Clone(manifest.canonical) }
func (manifest *ExposureManifest) Included() []SourceExposure { return slices.Clone(manifest.wire.Included) }
func (manifest *ExposureManifest) Omitted() []SourceExposure { return slices.Clone(manifest.wire.Omitted) }
func (manifest *ExposureManifest) ToolSchemaIDs() []string { return slices.Clone(manifest.wire.ToolSchemaIDs) }
func (manifest *ExposureManifest) SkillMetadataIDs() []string { return slices.Clone(manifest.wire.SkillMetadataIDs) }
func (manifest *ExposureManifest) PreloadedSkillBodyIDs() []string { return slices.Clone(manifest.wire.PreloadedSkillBodyIDs) }
func (manifest *ExposureManifest) SecretReferences() []string { return slices.Clone(manifest.wire.SecretReferences) }

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
	if wire.SchemaVersion != exposureSchemaVersion || !validHash(wire.SnapshotID) || strings.TrimSpace(wire.ToolSchemaGeneration) == "" || !sortedUniqueStrings(wire.ToolSchemaIDs) || !sortedUniqueStrings(wire.SkillMetadataIDs) || !sortedUniqueStrings(wire.PreloadedSkillBodyIDs) || !sortedUniqueStrings(wire.SecretReferences) {
		return ErrInvalidExposureManifest
	}
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
	return nil
}

func cloneExposureWire(wire exposureWire) exposureWire {
	wire.Included = slices.Clone(wire.Included)
	wire.Omitted = slices.Clone(wire.Omitted)
	wire.ToolSchemaIDs = slices.Clone(wire.ToolSchemaIDs)
	wire.SkillMetadataIDs = slices.Clone(wire.SkillMetadataIDs)
	wire.PreloadedSkillBodyIDs = slices.Clone(wire.PreloadedSkillBodyIDs)
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
