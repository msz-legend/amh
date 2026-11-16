package store

import (
	"strings"
	"time"

	"amh/internal/memory"
	"amh/internal/secrets"
)

// FilterOpts selects memories for the management UI. Empty fields mean
// "no filter on this axis"; Status=="" includes every status.
type FilterOpts struct {
	Status  memory.Status
	Project string
	Agent   string
	Type    memory.Type
	Q       string // keyword substring, AND across whitespace-separated terms
	Limit   int
}

// likeEscape mirrors the escaping used by DB.Search.
func likeEscape(t string) string {
	return "%" + strings.ReplaceAll(strings.ReplaceAll(t, `\`, `\\`), `%`, `\%`) + `%`
}

// Filter returns memories matching all supplied filters, newest first.
func (db *DB) Filter(o FilterOpts) ([]*memory.Memory, error) {
	q := `SELECT ` + memCols + ` FROM memories WHERE 1=1`
	var args []any
	if o.Status != "" {
		q += ` AND status = ?`
		args = append(args, string(o.Status))
	}
	if o.Project != "" {
		q += ` AND project_key = ?`
		args = append(args, o.Project)
	}
	if o.Agent != "" {
		q += ` AND source_agent = ?`
		args = append(args, o.Agent)
	}
	if o.Type != "" {
		q += ` AND type = ?`
		args = append(args, string(o.Type))
	}
	for _, t := range strings.Fields(o.Q) {
		pat := likeEscape(t)
		q += ` AND (content LIKE ? ESCAPE '\' OR type LIKE ? ESCAPE '\' OR project_key LIKE ? ESCAPE '\' OR source_agent LIKE ? ESCAPE '\')`
		args = append(args, pat, pat, pat, pat)
	}
	q += ` ORDER BY created_at DESC`
	if o.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, o.Limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*memory.Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Stats aggregates counts for the UI dashboard.
type Stats struct {
	Total     int            `json:"total"`
	ByStatus  map[string]int `json:"by_status"`
	ByAgent   map[string]int `json:"by_agent"`
	ByProject map[string]int `json:"by_project"`
}

// Stats computes aggregate counts over all memories.
func (db *DB) Stats() (*Stats, error) {
	s := &Stats{
		ByStatus:  map[string]int{},
		ByAgent:   map[string]int{},
		ByProject: map[string]int{},
	}
	count := func(col string, into map[string]int) error {
		rows, err := db.Query(`SELECT ` + col + `, COUNT(*) FROM memories GROUP BY ` + col)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			var n int
			if err := rows.Scan(&k, &n); err != nil {
				return err
			}
			into[k] = n
		}
		return rows.Err()
	}
	if err := count(`status`, s.ByStatus); err != nil {
		return nil, err
	}
	for _, n := range s.ByStatus {
		s.Total += n
	}
	if err := count(`source_agent`, s.ByAgent); err != nil {
		return nil, err
	}
	if err := count(`project_key`, s.ByProject); err != nil {
		return nil, err
	}
	return s, nil
}

// UpdateFields carries a UI edit; nil fields are left unchanged.
type UpdateFields struct {
	Content *string
	Project *string
	Type    *memory.Type
}

// Update applies a partial edit, revalidating (secrets guard, Normalize) and
// returning the updated memory. The caller is responsible for mirror sync.
func (db *DB) Update(id string, f UpdateFields) (*memory.Memory, error) {
	m, err := db.Get(id)
	if err != nil {
		return nil, err
	}
	if f.Content != nil {
		m.Content = strings.TrimSpace(*f.Content)
	}
	if f.Project != nil {
		m.ProjectKey = strings.TrimSpace(*f.Project)
	}
	if f.Type != nil {
		m.Type = *f.Type
	}
	if err := m.Normalize(m.CreatedAt); err != nil {
		return nil, err
	}
	if !secrets.Clean(m.Content) {
		return nil, ErrSecretDetected
	}
	res, err := db.Exec(`UPDATE memories SET content = ?, project_key = ?, type = ? WHERE id = ?`,
		m.Content, m.ProjectKey, string(m.Type), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return m, nil
}

// Upsert inserts or replaces a full memory row by id (reindex path: the
// Markdown cards pulled from git are the source of truth). FTS triggers keep
// the full-text index in sync.
func (db *DB) Upsert(m *memory.Memory) error {
	if err := m.Normalize(time.Now()); err != nil {
		return err
	}
	if !secrets.Clean(m.Content) {
		return ErrSecretDetected
	}
	_, err := db.Exec(`INSERT INTO memories (`+memCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		type = excluded.type, content = excluded.content,
		project_key = excluded.project_key, source_agent = excluded.source_agent,
		trust = excluded.trust, status = excluded.status,
		superseded_by = excluded.superseded_by,
		first_seen = excluded.first_seen, created_at = excluded.created_at`,
		m.ID, string(m.Type), m.Content, m.ProjectKey, m.SourceAgent,
		string(m.Trust), string(m.Status), m.SupersededBy,
		Time(m.FirstSeen), Time(m.CreatedAt))
	return err
}

// PruneExcept deletes every memory row whose id is not in keep (used by
// `amh reindex --prune` so the DB matches the card tree exactly).
func (db *DB) PruneExcept(keep map[string]bool) (int64, error) {
	rows, err := db.Query(`SELECT id FROM memories`)
	if err != nil {
		return 0, err
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		if !keep[id] {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, id := range stale {
		if _, err := db.Exec(`DELETE FROM memories WHERE id = ?`, id); err != nil {
			return 0, err
		}
	}
	return int64(len(stale)), nil
}
