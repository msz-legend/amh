package connect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyHooksFresh(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, ".claude", "settings.json")
	if err := ApplyHooks(sp, `C:\tools\amh.exe`); err != nil {
		t.Fatalf("ApplyHooks: %v", err)
	}
	data, _ := os.ReadFile(sp)
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	hooks := doc["hooks"].(map[string]any)
	for _, ev := range []string{"SessionStart", "SessionEnd"} {
		groups := hooks[ev].([]any)
		g := groups[0].(map[string]any)
		list := g["hooks"].([]any)
		h := list[0].(map[string]any)
		cmd := h["command"].(string)
		if !strings.Contains(cmd, "amh.exe") || !strings.Contains(cmd, "hook session-"+map[string]string{"SessionStart": "start", "SessionEnd": "end"}[ev]) {
			t.Errorf("%s command wrong: %s", ev, cmd)
		}
		if h["type"] != "command" {
			t.Errorf("%s type wrong", ev)
		}
	}
}

func TestApplyHooksPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, ".claude", "settings.json")
	existing := `{"hooks":{"SessionStart":[{"matcher":"*","hooks":[{"type":"command","command":"user-own.sh"}]}]},"model":"opus"}`
	if err := os.MkdirAll(filepath.Dir(sp), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(sp, []byte(existing), 0o644)
	if err := ApplyHooks(sp, `/usr/local/bin/amh`); err != nil {
		t.Fatalf("ApplyHooks: %v", err)
	}
	data, _ := os.ReadFile(sp)
	var doc map[string]any
	json.Unmarshal(data, &doc)
	if doc["model"] != "opus" {
		t.Error("top-level keys lost")
	}
	groups := doc["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (user + amh)", len(groups))
	}
	g0 := groups[0].(map[string]any)
	if g0["matcher"] != "*" {
		t.Error("user group lost")
	}
}
