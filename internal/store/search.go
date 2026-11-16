package store

import (
	"errors"
	"strings"

	"amh/internal/memory"
)

// Search does keyword recall over active memories. Primary path is the FTS5
// index (bm25 relevance + recency boost, newest favored within ~30 days).
// When FTS yields nothing (e.g. CJK queries, which unicode61 does not
// segment) or the query is not valid FTS syntax, it falls back to the
// B-stage AND-LIKE scan.
func (db *DB) Search(query, projectKey string, limit int) ([]*memory.Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	if hits, err := db.searchFTS(query, projectKey, limit); err == nil && len(hits) > 0 {
		return hits, nil
	}
	return db.searchLike(query, projectKey, limit)
}

var errEmptyFTSQuery = errors.New("store: empty FTS query")

// searchFTS runs the FTS5-ranked query. A bad query (syntax) or an empty
// token set surfaces as an error and the caller falls back to LIKE.
func (db *DB) searchFTS(query, projectKey string, limit int) ([]*memory.Memory, error) {
	q := buildFTSQuery(query)
	if q == "" {
		return nil, errEmptyFTSQuery
	}
	qs := `SELECT ` + memCols + ` FROM memories
		JOIN memories_fts ON memories.rowid = memories_fts.rowid
		WHERE memories_fts MATCH ? AND memories.status = 'active'`
	args := []any{q}
	if projectKey != "" {
		qs += ` AND memories.project_key = ?`
		args = append(args, projectKey)
	}
	// bm25() is lower-is-better; subtract a recency bonus so recent memories
	// rank above equally relevant older ones (bonus fades over 30 days).
	qs += ` ORDER BY bm25(memories_fts) - MAX(0.0, 2.0 - (julianday('now') - julianday(memories.created_at))/30.0)
		LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(qs, args...)
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

// buildFTSQuery turns the free-text query into an implicit-AND prefix query:
// each alphanumeric run becomes "tok"* (quoted, prefix-matched).
func buildFTSQuery(query string) string {
	var toks []string
	for _, t := range strings.FieldsFunc(query, func(r rune) bool {
		return !isWordRune(r)
	}) {
		t = strings.TrimSpace(t)
		if t != "" {
			toks = append(toks, `"`+strings.ReplaceAll(t, `"`, ``)+`"*`)
		}
	}
	return strings.Join(toks, " ")
}

func isWordRune(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9' || r > 127
}

// searchLike is the original B-stage fallback: every term must appear (AND,
// substring) in content, type, project_key or source_agent; newest first.
func (db *DB) searchLike(query, projectKey string, limit int) ([]*memory.Memory, error) {
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
