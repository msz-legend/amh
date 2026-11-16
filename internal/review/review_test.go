package review

import (
	"errors"
	"path/filepath"
	"testing"

	"amh/internal/memory"
	"amh/internal/store"
)

func newTestDB(t *testing.T) (*store.DB, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.MigrateUp(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, t.TempDir()
}

func insertOne(t *testing.T, db *store.DB, trust memory.Trust, status memory.Status) string {
	t.Helper()
	m := &memory.Memory{Type: memory.TypeDecision, Content: "test content " + string(trust),
		ProjectKey: "a/repo", SourceAgent: "codex", Trust: trust, Status: status}
	if err := db.Insert(m); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return m.ID
}

func TestDecideStatus(t *testing.T) {
	if s := DecideStatus(memory.TrustExplicit); s != memory.StatusActive {
		t.Errorf("explicit → %s", s)
	}
	if s := DecideStatus(memory.TrustUser); s != memory.StatusActive {
		t.Errorf("user → %s", s)
	}
	if s := DecideStatus(memory.TrustExtracted); s != memory.StatusProposed {
		t.Errorf("extracted → %s", s)
	}
}

func TestApproveRejectFlow(t *testing.T) {
	db, mirror := newTestDB(t)
	pid := insertOne(t, db, memory.TrustExtracted, memory.StatusProposed)

	// Approve: becomes active with trust=user, card mirrored.
	m, err := Approve(db, pid, mirror)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if m.Status != memory.StatusActive || m.Trust != memory.TrustUser {
		t.Errorf("approved state: status=%s trust=%s", m.Status, m.Trust)
	}
	got, _ := db.Get(pid)
	if got.Status != memory.StatusActive {
		t.Error("db status not updated")
	}

	// Approve again → ErrNotProposed.
	if _, err := Approve(db, pid, mirror); !errors.Is(err, ErrNotProposed) {
		t.Errorf("double approve: %v", err)
	}

	// Reject flow on a fresh proposed memory.
	pid2 := insertOne(t, db, memory.TrustExtracted, memory.StatusProposed)
	if _, err := Reject(db, pid2); err != nil {
		t.Fatalf("reject: %v", err)
	}
	got2, _ := db.Get(pid2)
	if got2.Status != memory.StatusRetired {
		t.Errorf("rejected status = %s", got2.Status)
	}
}

func TestListFIFO(t *testing.T) {
	db, _ := newTestDB(t)
	first := insertOne(t, db, memory.TrustExtracted, memory.StatusProposed)
	second := insertOne(t, db, memory.TrustExtracted, memory.StatusProposed)
	all, err := List(db, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 || all[0].ID != first || all[1].ID != second {
		t.Errorf("FIFO order wrong: %v", all)
	}
}

func TestRejectActiveFails(t *testing.T) {
	db, _ := newTestDB(t)
	aid := insertOne(t, db, memory.TrustExplicit, memory.StatusActive)
	if _, err := Reject(db, aid); !errors.Is(err, ErrNotProposed) {
		t.Errorf("reject active: %v", err)
	}
}
