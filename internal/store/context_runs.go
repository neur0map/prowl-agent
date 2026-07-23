package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

// ContextRun contains privacy-safe retrieval telemetry. It intentionally has no
// fields for question text, snippets, source bodies, or generated replies.
type ContextRun struct {
	ID              string `json:"id"`
	QueryHash       string `json:"query_hash"`
	HashVersion     string `json:"hash_version"`
	Mode            string `json:"mode"`
	BudgetTokens    int    `json:"budget_tokens"`
	BudgetBytes     int    `json:"budget_bytes"`
	EstimatedTokens int    `json:"estimated_tokens"`
	EstimatedBytes  int    `json:"estimated_bytes"`
	SelectedIDsJSON string `json:"selected_ids_json"`
	OmissionsJSON   string `json:"omissions_json"`
	TimingsJSON     string `json:"timings_json"`
	StrategyVersion string `json:"strategy_version"`
	Status          string `json:"status"`
	ErrorCode       string `json:"error_code,omitempty"`
	CreatedAt       string `json:"created_at"`
}

func (s *Store) SaveContextRun(run ContextRun) error {
	_, err := s.sql().Exec(`INSERT INTO context_runs(
		id,query_hash,hash_version,mode,budget_tokens,budget_bytes,estimated_tokens,estimated_bytes,
		selected_ids_json,omissions_json,timings_json,strategy_version,status,error_code,created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.QueryHash, run.HashVersion, run.Mode,
		run.BudgetTokens, run.BudgetBytes, run.EstimatedTokens, run.EstimatedBytes,
		run.SelectedIDsJSON, run.OmissionsJSON, run.TimingsJSON, run.StrategyVersion,
		run.Status, nullableString(run.ErrorCode), run.CreatedAt)
	return err
}

func (s *Store) ListContextRuns(limit int) ([]ContextRun, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.sql().Query(`SELECT id,query_hash,hash_version,mode,budget_tokens,budget_bytes,estimated_tokens,estimated_bytes,selected_ids_json,omissions_json,timings_json,strategy_version,status,IFNULL(error_code,''),created_at FROM context_runs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContextRun
	for rows.Next() {
		var run ContextRun
		if err := rows.Scan(&run.ID, &run.QueryHash, &run.HashVersion, &run.Mode, &run.BudgetTokens, &run.BudgetBytes, &run.EstimatedTokens, &run.EstimatedBytes, &run.SelectedIDsJSON, &run.OmissionsJSON, &run.TimingsJSON, &run.StrategyVersion, &run.Status, &run.ErrorCode, &run.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// ContextTraceSalt returns a workspace-local random salt without exposing it in
// any context packet or trace row.
func (s *Store) ContextTraceSalt() ([]byte, error) {
	var encoded string
	err := s.sql().QueryRow(`SELECT value FROM meta WHERE key='context_trace_salt'`).Scan(&encoded)
	if err == nil {
		return hex.DecodeString(encoded)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	var salt [32]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return nil, err
	}
	encoded = hex.EncodeToString(salt[:])
	if _, err := s.sql().Exec(`INSERT OR IGNORE INTO meta(key,value) VALUES('context_trace_salt',?)`, encoded); err != nil {
		return nil, err
	}
	if err := s.sql().QueryRow(`SELECT value FROM meta WHERE key='context_trace_salt'`).Scan(&encoded); err != nil {
		return nil, err
	}
	return hex.DecodeString(encoded)
}

func (s *Store) PruneContextRuns(before time.Time) (int64, error) {
	result, err := s.sql().Exec(`DELETE FROM context_runs WHERE created_at < ?`, before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
