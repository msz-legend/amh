// Package reindex implements F2: rebuild the local SQLite index from the
// Markdown card tree under <home>/memories. After the cards are synced via
// git (cards are the git-friendly source of truth), `amh reindex` makes the
// serving index match them again.
package reindex

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"amh/internal/memory"
	"amh/internal/store"
)

// Result summarizes one reindex pass.
type Result struct {
	Cards    int      // .md cards discovered
	Upserted int      // rows inserted or replaced
	Skipped  int      // unparseable/invalid cards
	Pruned   int64    // db-only rows removed (--prune)
	Errors   []string // "path: reason"
}

// ParseCard parses one rendered card back into a Memory.
func ParseCard(data string) (*memory.Memory, error) {
	lines := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("missing frontmatter")
	}
	end := -1
	fields := map[string]string{}
	for i := 1; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "---" {
			end = i
			break
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	get := func(k string) string { return fields[k] }
	unquote := func(s string) string {
		if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
			return s[1 : len(s)-1]
		}
		return s
	}
	m := &memory.Memory{
		ID:           get("id"),
		Type:         memory.Type(get("type")),
		Content:      strings.TrimSpace(strings.Join(lines[end+1:], "\n")),
		ProjectKey:   get("project"),
		SourceAgent:  get("source-agent"),
		Trust:        memory.Trust(get("trust")),
		Status:       memory.Status(get("status")),
		SupersededBy: unquote(get("superseded-by")),
	}
	var err error
	if m.FirstSeen, err = store.ParseTime(get("first-seen")); err != nil {
		return nil, fmt.Errorf("first-seen: %w", err)
	}
	if m.CreatedAt, err = store.ParseTime(get("created")); err != nil {
		return nil, fmt.Errorf("created: %w", err)
	}
	if m.ID == "" || m.Content == "" {
		return nil, fmt.Errorf("missing id or content")
	}
	if !m.Status.Valid() {
		return nil, fmt.Errorf("invalid status %q", string(m.Status))
	}
	return m, nil
}

// Run walks the card tree and upserts every parseable card into db. With
// prune, db rows absent from the card tree are deleted so the index matches
// the synced cards exactly.
func Run(db *store.DB, mirrorDir string, prune bool) (*Result, error) {
	res := &Result{}
	seen := map[string]bool{}
	err := filepath.WalkDir(mirrorDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		res.Cards++
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", path, rerr))
			res.Skipped++
			return nil
		}
		m, perr := ParseCard(string(data))
		if perr != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", path, perr))
			res.Skipped++
			return nil
		}
		if uerr := db.Upsert(m); uerr != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", path, uerr))
			res.Skipped++
			return nil
		}
		seen[m.ID] = true
		res.Upserted++
		return nil
	})
	if err != nil {
		return nil, err
	}
	if prune {
		n, perr := db.PruneExcept(seen)
		if perr != nil {
			return nil, perr
		}
		res.Pruned = n
	}
	return res, nil
}
