package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amh/internal/memory"
	"amh/internal/store"
)

func seedStore(t *testing.T, home string) {
	t.Helper()
	db, err := store.Open(filepath.Join(home, "amh.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.MigrateUp(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	m := &memory.Memory{Type: memory.TypeHandoff, Content: "seed handoff",
		ProjectKey: "github.com/x/repo", SourceAgent: "claude-code"}
	if err := db.Insert(m); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestSessionStartContext(t *testing.T) {
	home := t.TempDir()
	seedStore(t, home)
	p := Payload{SessionID: "abcdef12-3456", Cwd: `C:\work\repo`, HookEventName: "SessionStart"}
	o := Options{Home: home, Agent: "claude-code", Limit: 5,
		ResolveFn: func(dir string) string { return "github.com/x/repo" }}
	ctx, err := SessionStartContext(p, o)
	if err != nil {
		t.Fatalf("SessionStartContext: %v", err)
	}
	if !strings.Contains(ctx, "data-not-instruction") {
		t.Error("anti-poisoning banner missing")
	}
	if !strings.Contains(ctx, "seed handoff") {
		t.Errorf("seeded handoff not injected:\n%s", ctx)
	}

	out, err := SessionStartOutput(ctx)
	if err != nil {
		t.Fatalf("SessionStartOutput: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	specific := doc["hookSpecificOutput"].(map[string]any)
	if specific["hookEventName"] != "SessionStart" {
		t.Errorf("event = %v", specific["hookEventName"])
	}
	if specific["additionalContext"] == "" {
		t.Error("additionalContext empty")
	}
}

func TestSessionStartRequiresAgent(t *testing.T) {
	p := Payload{Cwd: "."}
	if _, err := SessionStartContext(p, Options{Home: t.TempDir()}); err == nil {
		t.Fatal("expected error without agent identity")
	}
}

func TestSessionEndSpool(t *testing.T) {
	home := t.TempDir()
	p := Payload{
		SessionID:      "sess-12345678",
		TranscriptPath: `C:\tmp\transcript.jsonl`,
		Cwd:            `C:\work\repo`,
		HookEventName:  "SessionEnd",
		Reason:         "clear",
	}
	fixed := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	o := Options{Home: home, Agent: "claude-code", NowFn: func() time.Time { return fixed },
		ResolveFn: func(dir string) string { return "github.com/x/repo" }}
	path, err := SessionEndSpool(p, o)
	if err != nil {
		t.Fatalf("SessionEndSpool: %v", err)
	}
	if !strings.Contains(filepath.Base(path), "claude-code-sess-123") {
		t.Errorf("spool name = %s", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read spool: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("spool not JSON: %v", err)
	}
	if doc["agent"] != "claude-code" || doc["project_key"] != "github.com/x/repo" {
		t.Errorf("spool fields: %v", doc)
	}
	if doc["transcript_path"] != p.TranscriptPath {
		t.Error("transcript path lost")
	}
}

func TestSessionEndRequiresAgent(t *testing.T) {
	p := Payload{Cwd: "."}
	if _, err := SessionEndSpool(p, Options{Home: t.TempDir()}); err == nil {
		t.Fatal("expected error without agent identity")
	}
}
