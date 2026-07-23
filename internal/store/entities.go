package store

import "strconv"

// EntityArtifactRow is transport-neutral artifact data for the generic facade.
type EntityArtifactRow struct {
	ID, URI, Kind, Title, Language, ContentHash, CanonicalPath string
	SourceSystem, SourceTable, SourceID                        string
}

// EntityNodeRow is transport-neutral node data for the generic facade.
type EntityNodeRow struct {
	ID, ArtifactID, StableKey, Kind, Name, Summary string
	AnchorStart, AnchorEnd                         int
	Deterministic                                  bool
	Confidence                                     float64
	SourceSystem, SourceTable, SourceID            string
}

// EntityRelationRow is transport-neutral relation data for the generic facade.
type EntityRelationRow struct {
	ID, FromID, ToID, Kind, Evidence    string
	Deterministic                       bool
	Confidence                          float64
	SourceSystem, SourceTable, SourceID string
}

// EntityArtifacts projects physical generic, legacy file, and knowledge rows.
func (s *Store) EntityArtifacts() ([]EntityArtifactRow, error) {
	rows, err := s.db.Query(`
		SELECT id,uri,kind,IFNULL(title,''),IFNULL(language,''),IFNULL(content_hash,''),IFNULL(canonical_path,''),source_adapter,'artifacts',id FROM artifacts
		UNION ALL
		SELECT 'artifact:file:' || id,'file://' || rel_path,'file',rel_path,lang,hash,rel_path,'legacy','files',CAST(id AS TEXT) FROM files
		UNION ALL
		SELECT 'artifact:knowledge:' || id,'prowl://knowledge/' || concept_id,'knowledge',title,'markdown',content_hash,path,'okf','knowledge_documents',id FROM knowledge_documents
		ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntityArtifactRow
	for rows.Next() {
		var row EntityArtifactRow
		if err := rows.Scan(&row.ID, &row.URI, &row.Kind, &row.Title, &row.Language, &row.ContentHash, &row.CanonicalPath, &row.SourceSystem, &row.SourceTable, &row.SourceID); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// EntityNodes projects physical nodes, legacy symbols, and OKF concepts.
func (s *Store) EntityNodes() ([]EntityNodeRow, error) {
	rows, err := s.db.Query(`
		SELECT id,IFNULL(artifact_id,''),stable_key,kind,name,IFNULL(summary,''),IFNULL(anchor_start,0),IFNULL(anchor_end,0),deterministic,confidence,'generic','nodes',id FROM nodes
		UNION ALL
		SELECT 'node:symbol:' || sy.id,'artifact:file:' || f.id,f.rel_path || '#L' || sy.start_line || ':' || sy.name,sy.kind,sy.name,IFNULL(sy.signature,''),sy.start_line,sy.end_line,1,1.0,'legacy','symbols',CAST(sy.id AS TEXT) FROM symbols sy JOIN files f ON f.id=sy.file_id
		UNION ALL
		SELECT 'node:knowledge:' || id,'artifact:knowledge:' || id,'concept:' || concept_id,'concept:' || okf_type,title,IFNULL(description,''),0,0,CASE WHEN review_state IN ('accepted','confirmed') THEN 1 ELSE 0 END,1.0,'okf','knowledge_documents',id FROM knowledge_documents
		ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntityNodeRow
	for rows.Next() {
		var row EntityNodeRow
		var deterministic int
		if err := rows.Scan(&row.ID, &row.ArtifactID, &row.StableKey, &row.Kind, &row.Name, &row.Summary, &row.AnchorStart, &row.AnchorEnd, &deterministic, &row.Confidence, &row.SourceSystem, &row.SourceTable, &row.SourceID); err != nil {
			return nil, err
		}
		row.Deterministic = deterministic != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

// EntityRelations projects physical generic relations and existing graph edges.
func (s *Store) EntityRelations() ([]EntityRelationRow, error) {
	rows, err := s.db.Query(`SELECT id,from_node,to_node,kind,IFNULL(evidence_anchor,''),deterministic,confidence FROM relations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []EntityRelationRow
	for rows.Next() {
		var row EntityRelationRow
		var deterministic int
		if err := rows.Scan(&row.ID, &row.FromID, &row.ToID, &row.Kind, &row.Evidence, &deterministic, &row.Confidence); err != nil {
			rows.Close()
			return nil, err
		}
		row.Deterministic = deterministic != 0
		row.SourceSystem, row.SourceTable, row.SourceID = "generic", "relations", row.ID
		out = append(out, row)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	legacy, err := s.db.Query(`SELECT id,src_type,src_id,IFNULL(dst_type,''),IFNULL(dst_id,0),kind,IFNULL(raw,''),IFNULL(file_id,0),IFNULL(line,0) FROM edges ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer legacy.Close()
	for legacy.Next() {
		var id, srcID, dstID, fileID int64
		var srcType, dstType, kind, raw string
		var line int
		if err := legacy.Scan(&id, &srcType, &srcID, &dstType, &dstID, &kind, &raw, &fileID, &line); err != nil {
			return nil, err
		}
		from := "node:" + srcType + ":" + strconv.FormatInt(srcID, 10)
		to := "node:" + dstType + ":" + strconv.FormatInt(dstID, 10)
		if dstType == "" || dstID == 0 {
			to = "unresolved:" + raw
		}
		out = append(out, EntityRelationRow{
			ID: "relation:edge:" + strconv.FormatInt(id, 10), FromID: from, ToID: to, Kind: kind,
			Evidence:      "file:" + strconv.FormatInt(fileID, 10) + "#L" + strconv.Itoa(line),
			Deterministic: true, Confidence: 1, SourceSystem: "legacy", SourceTable: "edges", SourceID: strconv.FormatInt(id, 10),
		})
	}
	return out, legacy.Err()
}
