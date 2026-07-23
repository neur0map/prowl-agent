package entity

import "github.com/prowl-agent/prowl-agent/internal/store"

// Repository projects generic records without dual-writing the legacy graph.
type Repository struct{ Store *store.Store }

func (r Repository) Artifacts() ([]Artifact, error) {
	rows, err := r.Store.EntityArtifacts()
	if err != nil {
		return nil, err
	}
	out := make([]Artifact, 0, len(rows))
	for _, row := range rows {
		out = append(out, Artifact{
			ID: row.ID, URI: row.URI, Kind: row.Kind, Title: row.Title, Language: row.Language,
			ContentHash: row.ContentHash, CanonicalPath: row.CanonicalPath,
			Provenance: Provenance{System: row.SourceSystem, Table: row.SourceTable, ID: row.SourceID},
		})
	}
	return out, nil
}

func (r Repository) Nodes() ([]Node, error) {
	rows, err := r.Store.EntityNodes()
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(rows))
	for _, row := range rows {
		out = append(out, Node{
			ID: row.ID, ArtifactID: row.ArtifactID, StableKey: row.StableKey, Kind: row.Kind,
			Name: row.Name, Summary: row.Summary, AnchorStart: row.AnchorStart, AnchorEnd: row.AnchorEnd,
			Deterministic: row.Deterministic, Confidence: row.Confidence,
			Provenance: Provenance{System: row.SourceSystem, Table: row.SourceTable, ID: row.SourceID},
		})
	}
	return out, nil
}

func (r Repository) Relations() ([]Relation, error) {
	rows, err := r.Store.EntityRelations()
	if err != nil {
		return nil, err
	}
	out := make([]Relation, 0, len(rows))
	for _, row := range rows {
		out = append(out, Relation{
			ID: row.ID, FromID: row.FromID, ToID: row.ToID, Kind: row.Kind, Evidence: row.Evidence,
			Deterministic: row.Deterministic, Confidence: row.Confidence,
			Provenance: Provenance{System: row.SourceSystem, Table: row.SourceTable, ID: row.SourceID},
		})
	}
	return out, nil
}
