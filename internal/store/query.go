package store

import (
	"strings"

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
