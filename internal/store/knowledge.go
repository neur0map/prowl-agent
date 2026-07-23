package store

import "database/sql"

// KnowledgeDocument is rebuildable metadata projected from canonical Markdown.
type KnowledgeDocument struct {
	ID           string `json:"id"`
	ConceptID    string `json:"concept_id"`
	Path         string `json:"path"`
	Type         string `json:"type"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	Resource     string `json:"resource,omitempty"`
	TagsJSON     string `json:"tags_json"`
	Timestamp    string `json:"timestamp,omitempty"`
	ReviewState  string `json:"review_state"`
	ContentHash  string `json:"content_hash"`
	MetadataJSON string `json:"metadata_json"`
}

// SourceAnchor is rebuildable evidence metadata for a knowledge document.
type SourceAnchor struct {
	ID           string
	KnowledgeID  string
	URI          string
	LineStart    int
	LineEnd      int
	ContentHash  string
	RegionHash   string
	Status       string
	CheckedAt    string
	MetadataJSON string
}

// ReplaceKnowledge atomically rebuilds canonical document and anchor metadata.
// Proposal review state is intentionally not touched.
func (s *Store) ReplaceKnowledge(documents []KnowledgeDocument, anchors []SourceAnchor) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM knowledge_documents`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO knowledge_documents(
		id,concept_id,path,okf_type,title,description,resource,tags_json,timestamp,review_state,content_hash,metadata_json
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	for _, doc := range documents {
		if _, err := stmt.Exec(doc.ID, doc.ConceptID, doc.Path, doc.Type, doc.Title, doc.Description, doc.Resource, doc.TagsJSON, doc.Timestamp, doc.ReviewState, doc.ContentHash, doc.MetadataJSON); err != nil {
			stmt.Close()
			return err
		}
	}
	if err := stmt.Close(); err != nil {
		return err
	}
	anchorStmt, err := tx.Prepare(`INSERT INTO source_anchors(
		id,knowledge_id,uri,line_start,line_end,content_hash,region_hash,status,checked_at,metadata_json
	) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	for _, anchor := range anchors {
		if _, err := anchorStmt.Exec(anchor.ID, anchor.KnowledgeID, anchor.URI, nullableInt(anchor.LineStart), nullableInt(anchor.LineEnd), anchor.ContentHash, anchor.RegionHash, anchor.Status, anchor.CheckedAt, anchor.MetadataJSON); err != nil {
			anchorStmt.Close()
			return err
		}
	}
	if err := anchorStmt.Close(); err != nil {
		return err
	}
	return tx.Commit()
}

// ListKnowledge returns canonical metadata in deterministic path order.
func (s *Store) ListKnowledge() ([]KnowledgeDocument, error) {
	rows, err := s.db.Query(`SELECT id,concept_id,path,okf_type,title,IFNULL(description,''),IFNULL(resource,''),tags_json,IFNULL(timestamp,''),review_state,content_hash,metadata_json FROM knowledge_documents ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KnowledgeDocument
	for rows.Next() {
		var doc KnowledgeDocument
		if err := rows.Scan(&doc.ID, &doc.ConceptID, &doc.Path, &doc.Type, &doc.Title, &doc.Description, &doc.Resource, &doc.TagsJSON, &doc.Timestamp, &doc.ReviewState, &doc.ContentHash, &doc.MetadataJSON); err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

// GetKnowledge resolves either a stable concept ID or a bundle-relative path.
func (s *Store) GetKnowledge(idOrPath string) (KnowledgeDocument, bool, error) {
	var doc KnowledgeDocument
	err := s.db.QueryRow(`SELECT id,concept_id,path,okf_type,title,IFNULL(description,''),IFNULL(resource,''),tags_json,IFNULL(timestamp,''),review_state,content_hash,metadata_json FROM knowledge_documents WHERE concept_id=? OR path=? LIMIT 1`, idOrPath, idOrPath).
		Scan(&doc.ID, &doc.ConceptID, &doc.Path, &doc.Type, &doc.Title, &doc.Description, &doc.Resource, &doc.TagsJSON, &doc.Timestamp, &doc.ReviewState, &doc.ContentHash, &doc.MetadataJSON)
	if err == sql.ErrNoRows {
		return KnowledgeDocument{}, false, nil
	}
	return doc, err == nil, err
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
