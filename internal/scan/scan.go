// Package scan locates historical session transcripts of each supported
// agent for cold-start import (`amh scan`). Format facts were verified
// against real local data (tasks/plan.md R0):
//
//	claude-code: ~/.claude/projects/<escaped-cwd>/<uuid>.jsonl
//	             records: {"type":"user"|"assistant", "message":{role,content}}
//	codex:       ~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl
//	             first line session_meta; message bodies live in
//	             {"type":"response_item","payload":{"type":"message",role,content}}
package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Transcript is one discovered session file.
type Transcript struct {
	Path      string
	Agent     string
	ModTime   time.Time
	SessionID string // best-effort, empty when unknown
}

// RecentTranscripts returns up to n most recent transcripts for an agent.
func RecentTranscripts(home string, agent string, n int) ([]Transcript, error) {
	switch agent {
	case "claude-code":
		return recentJSONL(filepath.Join(home, ".claude", "projects"), "claude-code", n)
	case "codex":
		return recentJSONL(filepath.Join(home, ".codex", "sessions"), "codex", n)
	default:
		return nil, nil
	}
}

// recentJSONL walks root recursively for *.jsonl files, newest first.
func recentJSONL(root, agent string, n int) ([]Transcript, error) {
	var out []Transcript
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtrees are skipped, not fatal
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		// Skip memory subdirectories of claude projects.
		if strings.Contains(path, string(filepath.Separator)+"memory"+string(filepath.Separator)) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, Transcript{Path: path, Agent: agent, ModTime: info.ModTime()})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// DigestTranscript adapts either agent's transcript into role-tagged text
// (same shape as extract.DigestTranscript input). Unknown records are
// skipped resiliently.
func DigestTranscript(agent, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var parts []string
	total := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			Message *struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			Payload *struct {
				Type    string          `json:"type"`
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		var role string
		var content json.RawMessage
		switch {
		case rec.Message != nil && rec.Message.Role != "":
			role, content = rec.Message.Role, rec.Message.Content // claude-code
		case rec.Payload != nil && rec.Payload.Type == "message" && rec.Payload.Role != "":
			role, content = rec.Payload.Role, rec.Payload.Content // codex
		}
		if role == "" || len(content) == 0 {
			continue
		}
		text := flatten(content)
		if text == "" {
			continue
		}
		parts = append(parts, role+": "+text)
		total += len(text)
		if total > maxDigestChars {
			break
		}
	}
	return strings.Join(parts, "\n"), nil
}

const maxDigestChars = 20000

// flatten handles string content and [{"type":"text"|"input_text"|"output_text","text":...}].
func flatten(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, blk := range blocks {
			if blk.Text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(blk.Text)
			}
		}
		return b.String()
	}
	return ""
}
