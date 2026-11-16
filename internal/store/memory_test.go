package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amh/internal/memory"
)

func openTestDB(t *testing.T) (*DB, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.MigrateUp(); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	return db, func() { db.Close() }
}

func TestMigrateUpDown(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	if v, err := db.SchemaVersion(); err != nil || v != 2 {
		t.Fatalf("version = %d, %v; want 2", v, err)
	}
	// Each MigrateDown rolls back exactly one step.
	if err := db.MigrateDown(); err != nil {
		t.Fatalf("MigrateDown v2: %v", err)
	}
	if v, _ := db.SchemaVersion(); v != 1 {
		t.Fatalf("version after first down = %d, want 1", v)
	}
	if err := db.MigrateDown(); err != nil {
		t.Fatalf("MigrateDown v1: %v", err)
	}
	if v, _ := db.SchemaVersion(); v != 0 {
		t.Fatalf("version after second down = %d, want 0", v)
	}
	if err := db.MigrateUp(); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if v, _ := db.SchemaVersion(); v != 2 {
		t.Fatalf("version after re-up = %d, want 2", v)
	}
}

func TestInsertGetList(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	m := &memory.Memory{
		Type:        memory.TypeDecision,
		Content:     "use pnpm, not npm",
		ProjectKey:  "github.com/example/repo",
		SourceAgent: "claude-code",
	}
	if err := db.Insert(m); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if m.ID == "" || m.Status != memory.StatusActive {
		t.Fatalf("Insert did not fill id/status: %+v", m)
	}

	got, err := db.Get(m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Content != m.Content || got.Type != m.Type || got.ProjectKey != m.ProjectKey {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
	if got.FirstSeen.IsZero() || got.CreatedAt.IsZero() {
		t.Errorf("timestamps lost in roundtrip")
	}

	// A proposed memory (extraction path) must not appear in active lists.
	proposed := &memory.Memory{
		Type: memory.TypeGotcha, Content: "watch out for X",
		Trust: memory.TrustExtracted, Status: memory.StatusProposed,
		SourceAgent: "codex",
	}
	if err := db.Insert(proposed); err != nil {
		t.Fatalf("Insert proposed: %v", err)
	}
	active, err := db.List(memory.StatusActive, "", 0)
	if err != nil {
		t.Fatalf("List active: %v", err)
	}
	if len(active) != 1 || active[0].ID != m.ID {
		t.Errorf("active list = %d items, want only the explicit one", len(active))
	}
	if all, err := db.List(memory.StatusProposed, "", 0); err != nil || len(all) != 1 {
		t.Errorf("proposed list = %d items, %v; want 1", len(all), err)
	}

	if _, err := db.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing: %v, want ErrNotFound", err)
	}
	if err := db.Insert(&memory.Memory{Content: ""}); !errors.Is(err, memory.ErrEmptyContent) {
		t.Errorf("Insert empty content: %v", err)
	}
	if err := db.Insert(&memory.Memory{Content: "token ghp_16CharactersEEEEEEEEEEEEEEE1"}); !errors.Is(err, ErrSecretDetected) {
		t.Errorf("Insert secret: %v, want ErrSecretDetected", err)
	}
}

func TestListProjectFilter(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	now := time.Now()
	for i, pk := range []string{"a/repo", "a/repo", "b/repo"} {
		m := &memory.Memory{
			Type: memory.TypeNote, Content: string(rune('a' + i)),
			ProjectKey: pk, CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		if err := db.Insert(m); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}
	got, err := db.List(memory.StatusActive, "a/repo", 0)
	if err != nil || len(got) != 2 {
		t.Fatalf("List a/repo = %d, %v; want 2", len(got), err)
	}
	if got[0].CreatedAt.Before(got[1].CreatedAt) {
		t.Errorf("List not newest-first")
	}
	top, _ := db.List(memory.StatusActive, "", 1)
	if len(top) != 1 || top[0].Content != "c" {
		t.Errorf("limit+ordering broken: %+v", top)
	}
}

func TestSearch(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	docs := []struct{ content, project string }{
		{"deployment uses wrangler with staging env", "a/repo"},
		{"rust builds need nightly toolchain", "b/repo"},
		{"pnpm store path is overridden on CI", "a/repo"},
	}
	for i, d := range docs {
		m := &memory.Memory{
			Type: memory.TypeNote, Content: d.content, ProjectKey: d.project,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		if err := db.Insert(m); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	// Retired docs must not be recallable.
	retired := &memory.Memory{Type: memory.TypeNote, Content: "secret wrangler prod keys"}
	if err := db.Insert(retired); err != nil {
		t.Fatal(err)
	}
	if err := db.Forget(retired.ID); err != nil {
		t.Fatal(err)
	}

	got, err := db.Search("wrangler", "", 0)
	if err != nil || len(got) != 1 || !strings.Contains(got[0].Content, "staging") {
		t.Errorf("Search wrangler = %d results, %v; want 1", len(got), err)
	}
	if got, _ = db.Search("wrangler", "a/repo", 0); len(got) != 1 {
		t.Errorf("Search wrangler+a/repo = %d, want 1", len(got))
	}
	if got, _ = db.Search("wrangler", "b/repo", 0); len(got) != 0 {
		t.Errorf("Search wrangler+b/repo = %d, want 0", len(got))
	}
	// AND semantics: both terms must hit.
	if got, _ = db.Search("pnpm CI", "", 0); len(got) != 1 {
		t.Errorf("Search pnpm CI = %d, want 1", len(got))
	}
	if got, _ = db.Search("nonexistentterm", "", 0); len(got) != 0 {
		t.Errorf("Search no-hit = %d, want 0", len(got))
	}
}
