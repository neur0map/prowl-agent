// Package entity exposes a generic evidence graph while preserving provenance
// back to legacy code-index and canonical knowledge records.
package entity

// Provenance identifies the underlying system record.
type Provenance struct {
	System string `json:"system"`
	Table  string `json:"table"`
	ID     string `json:"id"`
}

// Artifact is an addressable source or knowledge document.
type Artifact struct {
	ID, URI, Kind, Title, Language, ContentHash, CanonicalPath string
	Provenance                                                 Provenance `json:"provenance"`
}

// Node is a semantic item within an artifact.
type Node struct {
	ID, ArtifactID, StableKey, Kind, Name, Summary string
	AnchorStart, AnchorEnd                         int
	Deterministic                                  bool
	Confidence                                     float64
	Provenance                                     Provenance `json:"provenance"`
}

// Relation connects generic entity identifiers.
type Relation struct {
	ID, FromID, ToID, Kind, Evidence string
	Deterministic                    bool
	Confidence                       float64
	Provenance                       Provenance `json:"provenance"`
}
