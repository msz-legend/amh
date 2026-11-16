package store

import (
	"testing"
	"time"

	"amh/internal/memory"
)

func TestSearchFTSSyncAndFallback(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	m := &memory.Memory{Type: memory.TypeDecision, Content: "cache uses redis ttl 300"}
	if err := db.Insert(m); err != nil {
		t.Fatal(err)
	}
	// Edit via Update (webapi path): FTS must follow the trigger.
	newContent := "cache uses memcached ttl 600"
	if _, err := db.Update(m.ID, UpdateFields{Content: &newContent}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, _ := db.Search("memcached", "", 0); len(got) != 1 {
		t.Errorf("Search after edit = %d, want 1 (fts trigger sync)", len(got))
	}
	if got, _ := db.Search("redis", "", 0); len(got) != 0 {
		t.Errorf("Search stale term after edit = %d, want 0", len(got))
	}
	// Forget: FTS row removed via trigger.
	if err := db.Forget(m.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.Search("memcached", "", 0); len(got) != 0 {
		t.Errorf("Search after forget = %d, want 0", len(got))
	}
	// CJK: unicode61 does not segment it; LIKE fallback must still find it.
	cjk := &memory.Memory{Type: memory.TypeGotcha, Content: "部署时缓存失效需要预热"}
	if err := db.Insert(cjk); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.Search("缓存失效", "", 0); len(got) != 1 {
		t.Errorf("CJK fallback search = %d, want 1", len(got))
	}
}

func TestSearchRecencyBoost(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	old := &memory.Memory{Type: memory.TypeNote, Content: "release checklist step one",
		CreatedAt: time.Now().Add(-60 * 24 * time.Hour)}
	fresh := &memory.Memory{Type: memory.TypeNote, Content: "release checklist step one",
		CreatedAt: time.Now()}
	for _, m := range []*memory.Memory{old, fresh} {
		if err := db.Insert(m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.Search("release checklist", "", 0)
	if err != nil || len(got) != 2 {
		t.Fatalf("Search = %d, %v; want 2", len(got), err)
	}
	if got[0].ID != fresh.ID {
		t.Errorf("fresh memory should rank first under recency boost")
	}
}
