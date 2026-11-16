package connect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testEntry(agent string) Entry {
	return Entry{
		Agent:   agent,
		Command: `C:\tools\amh.exe`,
		Env:     map[string]string{"AMH_HOME": `C:\Users\me\.amh`, "AMH_AGENT": agent},
	}
}

func TestApplyClaudeJSONFresh(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, ".claude.json")
	tgt := Target{Path: tp, Format: "json", Scope: "global"}
	if err := Apply(tgt, testEntry("claude-code"), "amh"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	data, _ := os.ReadFile(tp)
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	servers := doc["mcpServers"].(map[string]any)
	amh := servers["amh"].(map[string]any)
	if amh["command"] != `C:\tools\amh.exe` {
		t.Errorf("command = %v", amh["command"])
	}
}

func TestApplyClaudeJSONPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, ".claude.json")
	existing := `{"mcpServers":{"other":{"command":"x"}},"theme":"dark"}`
	os.WriteFile(tp, []byte(existing), 0o644)
	tgt := Target{Path: tp, Format: "json", Scope: "global"}
	if err := Apply(tgt, testEntry("claude-code"), "amh"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	data, _ := os.ReadFile(tp)
	var doc map[string]any
	json.Unmarshal(data, &doc)
	if doc["theme"] != "dark" {
		t.Error("existing top-level keys lost")
	}
	servers := doc["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("existing other server lost")
	}
	if _, ok := servers["amh"]; !ok {
		t.Error("amh entry missing")
	}
	// backup written
	if _, err := os.Stat(tp + ".bak-"); err != nil && !os.IsNotExist(err) {
		t.Error("unexpected backup path form")
	}
	matches, _ := filepath.Glob(tp + ".bak-*")
	if len(matches) != 1 {
		t.Errorf("want 1 backup, got %d", len(matches))
	}
}

func TestApplyCodexTOMLFreshAndUpsert(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "config.toml")
	tgt := Target{Path: tp, Format: "toml", Scope: "global"}
	if err := Apply(tgt, testEntry("codex"), "amh"); err != nil {
		t.Fatalf("Apply fresh: %v", err)
	}
	data, _ := os.ReadFile(tp)
	if !strings.Contains(string(data), "[mcp_servers.amh]") {
		t.Error("table header missing")
	}
	if !strings.Contains(string(data), `AMH_AGENT = "codex"`) {
		t.Error("env missing")
	}

	// second apply (upsert) must not duplicate
	if err := Apply(tgt, testEntry("codex"), "amh"); err != nil {
		t.Fatalf("Apply upsert: %v", err)
	}
	data, _ = os.ReadFile(tp)
	if n := strings.Count(string(data), "[mcp_servers.amh]"); n != 1 {
		t.Errorf("header count = %d, want 1", n)
	}

	// upsert into a file with other tables preserves them
	os.WriteFile(tp, []byte("[model]\nname = \"gpt\"\n\n[mcp_servers.amh]\ncommand = \"old\"\nargs = [\"serve\"]\n\n[profile]\nx = 1\n"), 0o644)
	if err := Apply(tgt, testEntry("codex"), "amh"); err != nil {
		t.Fatalf("Apply upsert2: %v", err)
	}
	data, _ = os.ReadFile(tp)
	s := string(data)
	if !strings.Contains(s, "[model]") || !strings.Contains(s, `name = "gpt"`) || !strings.Contains(s, "[profile]") || !strings.Contains(s, "x = 1") {
		t.Error("other tables lost:\n" + s)
	}
	if strings.Contains(s, `"old"`) {
		t.Error("old command not replaced")
	}
	if n := strings.Count(s, "[mcp_servers.amh]"); n != 1 {
		t.Errorf("header count = %d after upsert2", n)
	}
	if strings.Index(s, "[mcp_servers.amh]") > strings.Index(s, "[profile]") {
		t.Errorf("amh block moved past profile:\n%s", s)
	}
}

func TestInspectAndPlan(t *testing.T) {
	dir := t.TempDir()
	// Inspect mutates slice ELEMENTS, so assert on the slice, never on a
	// struct copy that was passed by value into the slice literal.
	tgts := []Target{{Path: filepath.Join(dir, ".claude.json"), Format: "json", Scope: "global"}}
	Inspect(tgts, "amh")
	if tgts[0].Exists || tgts[0].Current != "" {
		t.Errorf("fresh target misdetected: %+v", tgts[0])
	}
	if err := os.WriteFile(tgts[0].Path, []byte(`{"mcpServers":{"amh":{"command":"old"}}}`), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	Inspect(tgts, "amh")
	if !tgts[0].Exists || tgts[0].Current == "" {
		t.Errorf("existing entry not detected: %+v", tgts[0])
	}
	p := Plan(testEntry("claude-code"))
	if !strings.Contains(p, "amh.exe") {
		t.Errorf("plan missing command: %s", p)
	}
}
