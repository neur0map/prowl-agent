package store

import "strconv"

// Stats are cumulative usage counters behind the savings report:
//   - Queries:       tool calls served
//   - AnswerBytes:   bytes of answers prowl actually returned
//   - BaselineBytes: bytes of the files those answers pointed at (what an agent
//     would otherwise have read to find the same things)
type Stats struct {
	Queries       int64 `json:"queries"`
	AnswerBytes   int64 `json:"answer_bytes"`
	BaselineBytes int64 `json:"baseline_bytes"`
}

// BumpStats atomically increments the usage counters by the given deltas.
func (s *Store) BumpStats(queries int, answerBytes, baselineBytes int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	bump := func(key string, delta int64) error {
		_, err := tx.Exec(
			`INSERT INTO meta(key,value) VALUES(?,?)
			 ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + ?`,
			key, strconv.FormatInt(delta, 10), delta)
		return err
	}
	if err := bump("stat_queries", int64(queries)); err != nil {
		return err
	}
	if err := bump("stat_answer_bytes", answerBytes); err != nil {
		return err
	}
	if err := bump("stat_baseline_bytes", baselineBytes); err != nil {
		return err
	}
	return tx.Commit()
}

// Stats reads the cumulative usage counters (zero when unset).
func (s *Store) Stats() (Stats, error) {
	get := func(key string) int64 {
		v, _ := s.GetMeta(key)
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return Stats{
		Queries:       get("stat_queries"),
		AnswerBytes:   get("stat_answer_bytes"),
		BaselineBytes: get("stat_baseline_bytes"),
	}, nil
}

// FileSizes maps every indexed file's project-relative path to its byte size.
func (s *Store) FileSizes() (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT rel_path, size FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]int64)
	for rows.Next() {
		var p string
		var sz int64
		if err := rows.Scan(&p, &sz); err != nil {
			return nil, err
		}
		m[p] = sz
	}
	return m, rows.Err()
}
