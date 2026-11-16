package consolidate

import (
	"path/filepath"
	"strings"
	"testing"

	"amh/internal/memory"
	"amh/internal/store"
)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateUp(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func ins(t *testing.T, db *store.DB, typ memory.Type, project, content string) *memory.Memory {
	t.Helper()
	m := &memory.Memory{Type: typ, Content: content, ProjectKey: project}
	if err := db.Insert(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDuplicateDetection(t *testing.T) {
	db := openDB(t)
	a := ins(t, db, memory.TypeDecision, "p/r", "deploy pipeline uses wrangler staging env with retry and rollback flags")
	b := ins(t, db, memory.TypeDecision, "p/r", "deploy pipeline uses wrangler staging env with retry and rollback notes")
	other := ins(t, db, memory.TypeDecision, "p/r", "rust builds need nightly toolchain now")

	rep, err := Run(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Suggestions) != 1 || rep.Suggestions[0].Kind != "duplicate" {
		t.Fatalf("suggestions = %+v; want 1 duplicate", rep.Suggestions)
	}
	s := rep.Suggestions[0]
	if s.KeepID != b.ID {
		t.Errorf("keep = %s; want newest %s", short(s.KeepID), short(b.ID))
	}
	if len(s.RetireIDs) != 1 || s.RetireIDs[0] != a.ID {
		t.Errorf("retire = %v; want [%s]", s.RetireIDs, short(a.ID))
	}
	if rep.Scanned != 3 {
		t.Errorf("scanned = %d; want 3", rep.Scanned)
	}
	_ = other
}

func TestContradictionDetection(t *testing.T) {
	db := openDB(t)
	a := ins(t, db, memory.TypeProjectRule, "p/r", "use sqlite wal mode for builds and migrations")
	b := ins(t, db, memory.TypeProjectRule, "p/r", "do not use sqlite wal mode for builds and migrations")
	_ = a

	rep, err := Run(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Suggestions) != 1 || rep.Suggestions[0].Kind != "contradiction" {
		t.Fatalf("suggestions = %+v; want 1 contradiction", rep.Suggestions)
	}
	if rep.Suggestions[0].KeepID != b.ID {
		t.Errorf("keep should be newest (%s)", short(b.ID))
	}
}

func TestCrossProjectNotFlagged(t *testing.T) {
	db := openDB(t)
	ins(t, db, memory.TypeDecision, "a/r", "deploy pipeline uses wrangler staging env with retry and rollback flags")
	ins(t, db, memory.TypeDecision, "b/r", "deploy pipeline uses wrangler staging env with retry and rollback notes")

	rep, err := Run(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Suggestions) != 0 {
		t.Errorf("cross-project pairs must not be flagged: %+v", rep.Suggestions)
	}
}

func TestReportRenderingProposalOnly(t *testing.T) {
	db := openDB(t)
	ins(t, db, memory.TypeDecision, "p/r", "deploy pipeline uses wrangler staging env with retry and rollback flags")
	ins(t, db, memory.TypeDecision, "p/r", "deploy pipeline uses wrangler staging env with retry and rollback notes")
	rep, err := Run(db)
	if err != nil {
		t.Fatal(err)
	}
	out := rep.Render()
	if !strings.Contains(out, "Proposal only") || !strings.Contains(out, "wrangler") {
		t.Errorf("report missing key text:\n%s", out)
	}
	// Proposal only: data must be untouched.
	mems, _ := db.List(memory.StatusActive, "", 0)
	if len(mems) != 2 {
		t.Errorf("data mutated by consolidate: %d active remain", len(mems))
	}
}
