package reindex

import (
	"path/filepath"
	"testing"
	"time"

	"amh/internal/memory"
	"amh/internal/store"
)

func openDB(t *testing.T, path string) *store.DB {
	t.Helper()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateUp(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestReindexRoundtrip(t *testing.T) {
	dir := t.TempDir()
	mirror := filepath.Join(dir, "memories")
	src := openDB(t, filepath.Join(dir, "src.db"))

	want := []*memory.Memory{
		{Type: memory.TypeDecision, Content: "deploy uses wrangler staging", ProjectKey: "a/repo",
			SourceAgent: "codex", Trust: memory.TrustExplicit, Status: memory.StatusActive,
			FirstSeen: time.Now().Add(-time.Hour), CreatedAt: time.Now().Add(-time.Hour)},
		{Type: memory.TypeGotcha, Content: "CI cache misses need warmup", ProjectKey: "b/repo",
			SourceAgent: "claude-code", Trust: memory.TrustUser, Status: memory.StatusRetired,
			SupersededBy: "deadbeef", FirstSeen: time.Now(), CreatedAt: time.Now()},
	}
	for _, m := range want {
		if err := src.InsertMirrored(m, mirror); err != nil {
			t.Fatalf("insert %s: %v", m.Content, err)
		}
	}

	// Fresh index (simulates another machine after git pull).
	dst := openDB(t, filepath.Join(dir, "dst.db"))
	res, err := Run(dst, mirror, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Cards != 2 || res.Upserted != 2 || res.Skipped != 0 {
		t.Fatalf("result = %+v; want 2/2/0", res)
	}
	for _, w := range want {
		got, err := dst.Get(w.ID)
		if err != nil {
			t.Fatalf("reindexed %s missing: %v", w.ID[:8], err)
		}
		if got.Content != w.Content || got.Status != w.Status ||
			got.SourceAgent != w.SourceAgent || got.SupersededBy != w.SupersededBy ||
			got.ProjectKey != w.ProjectKey || got.Trust != w.Trust || got.Type != w.Type {
			t.Errorf("roundtrip mismatch for %s: %+v", w.ID[:8], got)
		}
	}
	// FTS index must be usable right after reindex.
	hits, err := dst.Search("wrangler", "", 0)
	if err != nil || len(hits) != 1 {
		t.Errorf("search after reindex = %d, %v; want 1", len(hits), err)
	}
}

func TestReindexPrune(t *testing.T) {
	dir := t.TempDir()
	mirror := filepath.Join(dir, "memories")
	src := openDB(t, filepath.Join(dir, "src.db"))
	m := &memory.Memory{Type: memory.TypeNote, Content: "kept card", ProjectKey: "p/r",
		SourceAgent: "codex", Trust: memory.TrustExplicit, Status: memory.StatusActive}
	if err := src.InsertMirrored(m, mirror); err != nil {
		t.Fatal(err)
	}

	dst := openDB(t, filepath.Join(dir, "dst.db"))
	// DB-only row: exists in the index but has no card.
	extra := &memory.Memory{Type: memory.TypeNote, Content: "db-only orphan row", ProjectKey: "p/r",
		SourceAgent: "codex", Trust: memory.TrustExplicit, Status: memory.StatusActive}
	if err := dst.Insert(extra); err != nil {
		t.Fatal(err)
	}
	res, err := Run(dst, mirror, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Pruned != 1 || res.Upserted != 1 {
		t.Fatalf("result = %+v; want upserted=1 pruned=1", res)
	}
	if _, err := dst.Get(extra.ID); err != store.ErrNotFound {
		t.Errorf("orphan still present: %v", err)
	}
	if _, err := dst.Get(m.ID); err != nil {
		t.Errorf("card-backed row lost: %v", err)
	}
}

func TestParseCardRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "no frontmatter here", "---\nid: x\n---", "---\nid: x\ntype: note\n---\nbody but no created"} {
		if m, err := ParseCard(bad); err == nil {
			t.Errorf("ParseCard(%q) accepted %+v", bad, m)
		}
	}
}
