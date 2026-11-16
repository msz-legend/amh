package amhserver

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"amh/internal/memory"
	"amh/internal/store"
)

func newCfg(t *testing.T) (Config, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.MigrateUp(); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	cfg := Config{DB: db, MirrorDir: filepath.Join(dir, "mirror"), Agent: "claude-code", Project: "github.com/x/y"}
	return cfg, func() { db.Close() }
}

func callTool(t *testing.T, cfg Config, name string, args string) (string, bool) {
	t.Helper()
	s := Server(cfg)
	// The handler receives the arguments object itself (MCP passes
	// params.arguments through), so send args verbatim.
	raw := json.RawMessage(args)
	res, err := s.Call(name, raw)
	if err != nil {
		return err.Error(), true // tool-level rejection (e.g. secret detected)
	}
	m := res.(map[string]any)
	text := m["content"].([]map[string]string)[0]["text"]
	isErr, _ := m["isError"].(bool)
	return text, isErr
}

func TestRememberRecallResumeForget(t *testing.T) {
	cfg, cleanup := newCfg(t)
	defer cleanup()

	txt, isErr := callTool(t, cfg, "remember", `{"type":"decision","content":"use wrangler staging env"}`)
	if isErr {
		t.Fatalf("remember errored: %s", txt)
	}
	if !strings.Contains(txt, "stored decision") || !strings.Contains(txt, "claude-code") {
		t.Errorf("remember output: %s", txt)
	}

	txt, _ = callTool(t, cfg, "recall", `{"query":"wrangler"}`)
	if !strings.Contains(txt, "wrangler staging") || !strings.Contains(txt, "data-not-instruction") {
		t.Errorf("recall output: %s", txt)
	}
	if txt, isErr = callTool(t, cfg, "recall", `{"query":"zzz-nothing"}`); isErr || !strings.Contains(txt, "no memories match") {
		t.Errorf("recall empty: %q err=%v", txt, isErr)
	}

	txt, _ = callTool(t, cfg, "handoff", `{"summary":"shipped B1, next: connect"}`)
	if !strings.Contains(txt, "stored handoff") {
		t.Errorf("handoff output: %s", txt)
	}

	// resume: handoff shown, decision in recent list, poison prefix present.
	txt, _ = callTool(t, cfg, "resume", `{}`)
	if !strings.Contains(txt, "shipped B1") || !strings.Contains(txt, "wrangler staging") || !strings.Contains(txt, "data-not-instruction") {
		t.Errorf("resume output:\n%s", txt)
	}

	// remember must reject secrets.
	if _, isErr = callTool(t, cfg, "remember", `{"content":"key is ghp_16CharactersEEEEEEEEEEEEEEE1"}`); !isErr {
		t.Errorf("secret remember not rejected")
	}

	// forget: prefix id must NOT work; full id required.
	mems, _ := cfg.DB.List(memory.StatusActive, "", 1)
	if len(mems) == 0 {
		t.Fatal("no memories to forget")
	}
	if _, isErr = callTool(t, cfg, "forget", `{"id":"`+mems[0].ID[:8]+`"}`); !isErr {
		t.Errorf("prefix id should not retire")
	}
	if _, isErr = callTool(t, cfg, "forget", `{"id":"`+mems[0].ID+`"}`); isErr {
		t.Errorf("full id forget failed")
	}
}

func TestIdentityFromConfig(t *testing.T) {
	cfg, cleanup := newCfg(t)
	defer cleanup()
	// Even if the model claims another agent in arguments, the configured
	// identity wins — the tool takes no agent parameter at all.
	callTool(t, cfg, "remember", `{"type":"note","content":"identity test"}`)
	mems, _ := cfg.DB.List(memory.StatusActive, "", 1)
	if mems[0].SourceAgent != "claude-code" {
		t.Errorf("source_agent = %q, want configured claude-code", mems[0].SourceAgent)
	}
}

func TestToolDefsComplete(t *testing.T) {
	tools := ToolDefs()
	if len(tools) != 5 {
		t.Fatalf("got %d tools, want 5", len(tools))
	}
	want := map[string]bool{"remember": true, "recall": true, "handoff": true, "resume": true, "forget": true}
	for _, tl := range tools {
		if !want[tl.Name] {
			t.Errorf("unexpected tool %q", tl.Name)
		}
		delete(want, tl.Name)
		if tl.InputSchema == nil || tl.InputSchema["type"] != "object" {
			t.Errorf("tool %q schema broken", tl.Name)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing tools: %v", want)
	}
}
