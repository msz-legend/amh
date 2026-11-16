package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"amh/internal/memory"
	"amh/internal/secrets"
)

// ErrSecretDetected is returned when content looks like a credential.
var ErrSecretDetected = errors.New("store: content looks like a secret; refusing to store")

// ErrNotFound is returned when a memory id does not exist.
var ErrNotFound = errors.New("store: memory not found")

// memoryRow mirrors the memories table columns.
type memoryRow struct {
	ID           string
	Type         string
	Content      string
	ProjectKey   string
	SourceAgent  string
	Trust        string
	Status       string
	SupersededBy string
	FirstSeen    string
	CreatedAt    string
}

func (r memoryRow) toMemory() (*memory.Memory, error) {
	m := &memory.Memory{
		ID:           r.ID,
		Type:         memory.Type(r.Type),
		Content:      r.Content,
		ProjectKey:   r.ProjectKey,
		SourceAgent:  r.SourceAgent,
		Trust:        memory.Trust(r.Trust),
		Status:       memory.Status(r.Status),
		SupersededBy: r.SupersededBy,
	}
	var err error
	if m.FirstSeen, err = ParseTime(r.FirstSeen); err != nil {
		return nil, fmt.Errorf("store: first_seen %q: %w", r.FirstSeen, err)
	}
	if m.CreatedAt, err = ParseTime(r.CreatedAt); err != nil {
		return nil, fmt.Errorf("store: created_at %q: %w", r.CreatedAt, err)
	}
	return m, nil
}

const memCols = `id, type, content, project_key, source_agent, trust, status, superseded_by, first_seen, created_at`

// Insert validates and stores a memory in one transaction.
func (db *DB) Insert(m *memory.Memory) error {
	if err := m.Normalize(time.Now()); err != nil {
		return err
	}
	if !secrets.Clean(m.Content) {
		return ErrSecretDetected
	}
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.Status == "" {
		m.Status = memory.StatusActive
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO memories (`+memCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		m.ID, string(m.Type), m.Content, m.ProjectKey, m.SourceAgent,
		string(m.Trust), string(m.Status), m.SupersededBy,
		Time(m.FirstSeen), Time(m.CreatedAt)); err != nil {
		return err
	}
	return tx.Commit()
}

// Get fetches one memory by id.
func (db *DB) Get(id string) (*memory.Memory, error) {
	return scanMemory(db.QueryRow(`SELECT `+memCols+` FROM memories WHERE id = ?`, id))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMemory(row rowScanner) (*memory.Memory, error) {
	var r memoryRow
	if err := row.Scan(&r.ID, &r.Type, &r.Content, &r.ProjectKey, &r.SourceAgent,
		&r.Trust, &r.Status, &r.SupersededBy, &r.FirstSeen, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r.toMemory()
}

// List returns memories filtered by status and optional project key, newest first.
func (db *DB) List(status memory.Status, projectKey string, limit int) ([]*memory.Memory, error) {
	q := `SELECT ` + memCols + ` FROM memories WHERE status = ?`
	args := []any{string(status)}
	if projectKey != "" {
		q += ` AND project_key = ?`
		args = append(args, projectKey)
	}
	q += ` ORDER BY created_at DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
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

// Search does keyword recall over active memories: every whitespace-separated
// term must appear in content, type, project_key or source_agent (AND semantics).
// B-stage simple version; FTS5 + recency boost lands in E1.
func (db *DB) Search(query, projectKey string, limit int) ([]*memory.Memory, error) {
	terms := strings.Fields(query)
	if limit <= 0 {
		limit = 10
	}
	q := `SELECT ` + memCols + ` FROM memories WHERE status = ?`
	args := []any{string(memory.StatusActive)}
	for _, t := range terms {
		pat := `%` + strings.ReplaceAll(strings.ReplaceAll(t, `\`, `\\`), `%`, `\%`) + `%`
		q += ` AND (content LIKE ? ESCAPE '\' OR type LIKE ? ESCAPE '\' OR project_key LIKE ? ESCAPE '\' OR source_agent LIKE ? ESCAPE '\')`
		args = append(args, pat, pat, pat, pat)
	}
	if projectKey != "" {
		q += ` AND project_key = ?`
		args = append(args, projectKey)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
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
