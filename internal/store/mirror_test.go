package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amh/internal/memory"
)

func TestMirrorSlug(t *testing.T) {
	cases := map[string]string{
		"github.com/example/repo": "github.com-example-repo",
		"":                        "default",
		"C:\\Users\\x\\proj":      "c-users-x-proj",
	}
	for in, want := range cases {
		if got := MirrorSlug(in); got != want {
			t.Errorf("MirrorSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderCard(t *testing.T) {
	m := &memory.Memory{
		ID: "abc123", Type: memory.TypeDecision, Content: "use pnpm",
		ProjectKey: "github.com/x/y", SourceAgent: "codex", Trust: memory.TrustUser,
		Status: memory.StatusActive, SupersededBy: "def456",
	}
	card := RenderCard(m)
	for _, want := range []string{
		"id: abc123", "type: decision", "status: active",
		`superseded-by: "def456"`, "source-agent: codex", "\nuse pnpm\n",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("card missing %q:\n%s", want, card)
		}
	}
}

func TestInsertMirroredCompensates(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	dir := t.TempDir()

	m := &memory.Memory{Type: memory.TypeNote, Content: "hello", ProjectKey: "p/one"}
	if err := db.InsertMirrored(m, dir); err != nil {
		t.Fatalf("InsertMirrored: %v", err)
	}
	card, err := os.ReadFile(CardPath(dir, m))
	if err != nil {
		t.Fatalf("card missing: %v", err)
	}
	if !strings.Contains(string(card), "hello") {
		t.Errorf("card content wrong:\n%s", card)
	}

	// Force a mirror failure by making the target path a file, not a dir:
	// writing <dir>/p-conflict/<id>.md fails because p-conflict is a file.
	conflict := filepath.Join(dir, "p-conflict")
	if err := os.WriteFile(conflict, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup conflict: %v", err)
	}
	bad := &memory.Memory{Type: memory.TypeNote, Content: "bad", ProjectKey: "p/conflict"}
	err = db.InsertMirrored(bad, dir)
	var ce *cardErr
	if !errors.As(err, &ce) {
		t.Fatalf("want cardErr, got %v", err)
	}
	if _, err := db.Get(bad.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("compensation failed: row still present (%v)", err)
	}
}

func TestSupersedeChain(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	dir := t.TempDir()

	v1 := &memory.Memory{Type: memory.TypeDecision, Content: "v1", ProjectKey: "p/one", SourceAgent: "claude-code"}
	if err := db.InsertMirrored(v1, dir); err != nil {
		t.Fatalf("insert v1: %v", err)
	}
	v2 := &memory.Memory{Type: memory.TypeDecision, Content: "v2", ProjectKey: "p/one", SourceAgent: "codex"}
	if err := db.Supersede(v1.ID, v2, dir); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	got1, err := db.Get(v1.ID)
	if err != nil {
		t.Fatalf("get v1: %v", err)
	}
	if got1.Status != memory.StatusRetired || got1.SupersededBy != v2.ID {
		t.Errorf("v1 chain broken: %+v", got1)
	}
	got2, _ := db.Get(v2.ID)
	if got2.Status != memory.StatusActive {
		t.Errorf("v2 not active: %+v", got2)
	}

	// Mirror reflects the chain on both cards.
	c1, _ := os.ReadFile(CardPath(dir, v1))
	if !strings.Contains(string(c1), "status: retired") || !strings.Contains(string(c1), v2.ID) {
		t.Errorf("v1 card not updated:\n%s", c1)
	}
	c2, _ := os.ReadFile(CardPath(dir, v2))
	if !strings.Contains(string(c2), "status: active") {
		t.Errorf("v2 card wrong:\n%s", c2)
	}

	// Cannot supersede an already-retired memory.
	v3 := &memory.Memory{Type: memory.TypeNote, Content: "v3"}
	if err := db.Supersede(v1.ID, v3, ""); err == nil {
		t.Errorf("superseding retired memory should fail")
	}
	// Unknown id.
	if err := db.Supersede("missing", v3, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("supersede missing: %v", err)
	}
}

func TestForget(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	m := &memory.Memory{Type: memory.TypeNote, Content: "x"}
	if err := db.Insert(m); err != nil {
		t.Fatal(err)
	}
	if err := db.Forget(m.ID); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	got, _ := db.Get(m.ID)
	if got.Status != memory.StatusRetired {
		t.Errorf("status = %s, want retired", got.Status)
	}
	if err := db.Forget(m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("double forget: %v", err)
	}
}
