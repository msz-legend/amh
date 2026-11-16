package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"amh/internal/memory"
)

// mirrorSlugRE strips anything unsafe for a filesystem path segment.
var mirrorSlugRE = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// MirrorSlug turns a project key into a safe directory name.
func MirrorSlug(projectKey string) string {
	s := mirrorSlugRE.ReplaceAllString(strings.ToLower(projectKey), "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		s = "default"
	}
	return s
}

// CardPath returns the Markdown mirror path for a memory:
// <dir>/<project-slug>/<id>.md
func CardPath(dir string, m *memory.Memory) string {
	return filepath.Join(dir, MirrorSlug(m.ProjectKey), m.ID+".md")
}

// Frontmatter renders the YAML front matter of a memory card.
func Frontmatter(m *memory.Memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nid: %s\ntype: %s\nproject: %s\nsource-agent: %s\ntrust: %s\nstatus: %s\nsuperseded-by: %q\nfirst-seen: %s\ncreated: %s\n---\n\n",
		m.ID, m.Type, m.ProjectKey, m.SourceAgent, m.Trust, m.Status,
		m.SupersededBy, Time(m.FirstSeen), Time(m.CreatedAt))
	return b.String()
}

// RenderCard renders the full Markdown card for a memory.
func RenderCard(m *memory.Memory) string {
	return Frontmatter(m) + m.Content + "\n"
}

// writeCardFile atomically writes one card (temp file + rename).
func writeCardFile(dir string, m *memory.Memory) error {
	path := CardPath(dir, m)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(RenderCard(m)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func removeCardFile(dir string, m *memory.Memory) error {
	err := os.Remove(CardPath(dir, m))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// cardErr wraps mirror write failures so callers can distinguish them.
type cardErr struct{ err error }

func (e *cardErr) Error() string { return "mirror: " + e.err.Error() }
func (e *cardErr) Unwrap() error { return e.err }

// InsertMirrored inserts a memory and mirrors its card. If the mirror write
// fails after the DB commit, the insert is compensated (deleted) so both
// tracks stay consistent.
func (db *DB) InsertMirrored(m *memory.Memory, mirrorDir string) error {
	if mirrorDir == "" {
		return db.Insert(m)
	}
	if err := db.Insert(m); err != nil {
		return err
	}
	if err := writeCardFile(mirrorDir, m); err != nil {
		// Compensate: remove the row, then surface the mirror failure.
		if _, delErr := db.Exec(`DELETE FROM memories WHERE id = ?`, m.ID); delErr != nil {
			return fmt.Errorf("mirror failed (%v) AND compensation failed (%v)", err, delErr)
		}
		return &cardErr{err}
	}
	return nil
}

// Supersede retires oldID and inserts next as the active replacement in one
// transaction. With a mirrorDir, both cards are rewritten to reflect the
// chain; a mirror failure rolls the DB change back before commit.
func (db *DB) Supersede(oldID string, next *memory.Memory, mirrorDir string) (err error) {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var oldStatus string
	err = tx.QueryRow(`SELECT status FROM memories WHERE id = ?`, oldID).Scan(&oldStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if oldStatus != string(memory.StatusActive) {
		return fmt.Errorf("store: cannot supersede memory %s in status %s", oldID, oldStatus)
	}

	if err := next.Normalize(time.Now()); err != nil {
		return err
	}
	if next.ID == "" {
		next.ID = NewID()
	}
	if next.Status == "" {
		next.Status = memory.StatusActive
	}
	if _, err := tx.Exec(`INSERT INTO memories (`+memCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		next.ID, string(next.Type), next.Content, next.ProjectKey, next.SourceAgent,
		string(next.Trust), string(next.Status), next.SupersededBy,
		Time(next.FirstSeen), Time(next.CreatedAt)); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE memories SET status = ?, superseded_by = ? WHERE id = ?`,
		string(memory.StatusRetired), next.ID, oldID); err != nil {
		return err
	}

	// Mirror writes happen inside the transaction window: on failure we
	// return before commit, so SQLite rolls back automatically.
	if mirrorDir != "" {
		updated := *next
		if err := writeCardFile(mirrorDir, &updated); err != nil {
			return &cardErr{err}
		}
		var oldRow memoryRow
		if err := tx.QueryRow(`SELECT `+memCols+` FROM memories WHERE id = ?`, oldID).
			Scan(&oldRow.ID, &oldRow.Type, &oldRow.Content, &oldRow.ProjectKey, &oldRow.SourceAgent,
				&oldRow.Trust, &oldRow.Status, &oldRow.SupersededBy, &oldRow.FirstSeen, &oldRow.CreatedAt); err != nil {
			return err
		}
		oldMem, err := oldRow.toMemory()
		if err != nil {
			return err
		}
		if err := writeCardFile(mirrorDir, oldMem); err != nil {
			return &cardErr{err}
		}
	}
	return tx.Commit()
}

// Forget retires a memory without a replacement (soft delete).
func (db *DB) Forget(id string) error {
	res, err := db.Exec(`UPDATE memories SET status = ? WHERE id = ? AND status = ?`,
		string(memory.StatusRetired), id, string(memory.StatusActive))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SyncMirror rewrites the card for one memory (e.g. after Forget or review).
func SyncMirror(mirrorDir string, m *memory.Memory) error {
	return writeCardFile(mirrorDir, m)
}
