// Package review implements the minimal approval surface of amh (PRD §8):
// explicit writes take effect immediately; extracted memories land as
// proposed and need human review before they are served to agents.
package review

import (
	"errors"
	"fmt"

	"amh/internal/memory"
	"amh/internal/store"
)

// ErrNotProposed is returned when approving/rejecting a memory that is not
// in proposed status.
var ErrNotProposed = errors.New("review: memory is not in proposed status")

// DecideStatus maps trust to the initial lifecycle status.
func DecideStatus(trust memory.Trust) memory.Status {
	switch trust {
	case memory.TrustExplicit, memory.TrustUser:
		return memory.StatusActive
	default: // TrustExtracted — model output needs human sign-off
		return memory.StatusProposed
	}
}

// List returns all proposed memories (oldest first, so the queue drains in
// arrival order).
func List(db *store.DB, limit int) ([]*memory.Memory, error) {
	all, err := db.List(memory.StatusProposed, "", limit)
	if err != nil {
		return nil, err
	}
	// db.List is newest-first; reverse for a FIFO review queue.
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all, nil
}

// Approve promotes a proposed memory to active with trust=user and mirrors
// its card.
func Approve(db *store.DB, id, mirrorDir string) (*memory.Memory, error) {
	return transition(db, id, mirrorDir, func(m *memory.Memory) {
		m.Status = memory.StatusActive
		m.Trust = memory.TrustUser
	})
}

// Reject retires a proposed memory without mirroring (nothing was served,
// keep the row as history).
func Reject(db *store.DB, id string) (*memory.Memory, error) {
	m, err := db.Get(id)
	if err != nil {
		return nil, err
	}
	if m.Status != memory.StatusProposed {
		return nil, fmt.Errorf("%w (id=%s status=%s)", ErrNotProposed, id, m.Status)
	}
	if _, err := db.Exec(`UPDATE memories SET status = ? WHERE id = ?`,
		string(memory.StatusRetired), id); err != nil {
		return nil, err
	}
	m.Status = memory.StatusRetired
	return m, nil
}

func transition(db *store.DB, id, mirrorDir string, apply func(*memory.Memory)) (*memory.Memory, error) {
	m, err := db.Get(id)
	if err != nil {
		return nil, err
	}
	if m.Status != memory.StatusProposed {
		return nil, fmt.Errorf("%w (id=%s status=%s)", ErrNotProposed, id, m.Status)
	}
	apply(m)
	res, err := db.Exec(`UPDATE memories SET status = ?, trust = ? WHERE id = ?`,
		string(m.Status), string(m.Trust), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, store.ErrNotFound
	}
	if mirrorDir != "" {
		if err := store.SyncMirror(mirrorDir, m); err != nil {
			return nil, fmt.Errorf("review: mirror sync: %w", err)
		}
	}
	return m, nil
}
