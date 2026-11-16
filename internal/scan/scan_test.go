package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecentTranscriptsClaudeShape(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, ".claude", "projects", "C--Users-me-repo")
	os.MkdirAll(proj, 0o755)
	write := func(name, body string) {
		os.WriteFile(filepath.Join(proj, name), []byte(body), 0o644)
	}
	write("aaa.jsonl", `{"type":"user","cwd":"C:\\me\\repo","message":{"role":"user","content":"hello"}}`)
	write("bbb.jsonl", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi there"}]}}`)
	os.MkdirAll(filepath.Join(proj, "memory"), 0o755)
	write(filepath.Join("memory", "note.jsonl"), `{"type":"user","message":{"role":"user","content":"should be skipped"}}`)

	ts, err := RecentTranscripts(home, "claude-code", 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(ts) != 2 {
		t.Fatalf("got %d transcripts (memory/ not skipped?)", len(ts))
	}
	digest, err := DigestTranscript("claude-code", ts[0].Path)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if !strings.Contains(digest, "user: hello") && !strings.Contains(digest, "assistant: hi there") {
		t.Errorf("digest wrong:\n%s", digest)
	}
}

func TestRecentTranscriptsCodexShape(t *testing.T) {
	home := t.TempDir()
	day := filepath.Join(home, ".codex", "sessions", "2026", "11", "16")
	os.MkdirAll(day, 0o755)
	lines := []string{
		`{"type":"session_meta","payload":{"session_id":"abc123","cwd":"C:\\me\\repo","originator":"codex-cli"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"decided to use redis for sessions"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"noted"}]}}`,
		`{"type":"token_usage_record","payload":{"input":1}}`,
	}
	os.WriteFile(filepath.Join(day, "rollout-2026-11-16T10h00m00-abc123.jsonl"), []byte(strings.Join(lines, "\n")), 0o644)

	ts, err := RecentTranscripts(home, "codex", 5)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(ts) != 1 {
		t.Fatalf("got %d", len(ts))
	}
	digest, err := DigestTranscript("codex", ts[0].Path)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if !strings.Contains(digest, "user: decided to use redis") {
		t.Errorf("codex digest wrong:\n%s", digest)
	}
	if strings.Contains(digest, "token_usage") {
		t.Error("non-message record leaked into digest")
	}
}

func TestUnknownAgent(t *testing.T) {
	ts, err := RecentTranscripts(t.TempDir(), "mystery", 5)
	if err != nil || len(ts) != 0 {
		t.Errorf("unknown agent should be empty/no-error: %v %v", ts, err)
	}
}

func TestDigestSkipsJunk(t *testing.T) {
	d := filepath.Join(t.TempDir(), "x.jsonl")
	os.WriteFile(d, []byte("not json\n{\"type\":\"user\"}\n"), 0o644)
	out, err := DigestTranscript("claude-code", d)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if out != "" {
		t.Errorf("junk digest = %q", out)
	}
}
