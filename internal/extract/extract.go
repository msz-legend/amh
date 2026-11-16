// Package extract turns session-end spool files into proposed memories via
// an OpenAI-compatible chat endpoint (local Ollama by default; override with
// AMH_EXTRACT_BASE_URL / AMH_EXTRACT_API_KEY / AMH_EXTRACT_MODEL).
package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"amh/internal/memory"
	"amh/internal/review"
	"amh/internal/secrets"
	"amh/internal/store"
)

// Spool is the session-end capture record (schema amh.spool.v1).
type Spool struct {
	Schema         string `json:"schema"`
	Agent          string `json:"agent"`
	ProjectKey     string `json:"project_key"`
	TranscriptPath string `json:"transcript_path"`
	SessionID      string `json:"session_id"`
	SpooledAt      string `json:"spooled_at"`
}

// Config controls the extraction worker.
type Config struct {
	BaseURL string // default http://127.0.0.1:11434/v1
	APIKey  string // optional Authorization: Bearer
	Model   string // default qwen3:4b
	Timeout time.Duration
}

const maxTranscriptChars = 20000

var errUnreachable = errors.New("extract: model endpoint unreachable")

// DigestTranscript reads a JSONL transcript and extracts the conversational
// text (user/assistant message bodies), truncated to maxTranscriptChars.
// Unknown record shapes are skipped — adapters must be resilient.
func DigestTranscript(path string) (string, error) {
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
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec.Message.Role == "" {
			continue
		}
		text := flattenContent(rec.Message.Content)
		if text == "" {
			continue
		}
		parts = append(parts, rec.Message.Role+": "+text)
		total += len(text)
		if total > maxTranscriptChars {
			break
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, "\n"), nil
}

// flattenContent handles string or [{"type":"text","text":...}] content.
func flattenContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
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
			if blk.Type == "text" && blk.Text != "" {
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

const extractPrompt = `You extract durable, reusable memories from an AI coding-agent session transcript.

Return ONLY a JSON array (no prose, no markdown fence). Each element:
{"type": "<decision|gotcha|preference|project-rule|note>", "content": "<one self-contained sentence>"}

Rules:
- Only durable facts: decisions made, gotchas discovered, user preferences, project rules.
- Exclude small talk, tool output, secrets, and anything already obvious from code.
- Skip if nothing durable happened: return [].
- Content must be self-contained (no "as discussed above").`

// Candidate is one model-proposed memory.
type Candidate struct {
	Type    memory.Type `json:"type"`
	Content string      `json:"content"`
}

// Extract calls the model and parses candidates. Network/parse failures are
// reported as error; the spool stays pending for a later retry.
func Extract(ctx context.Context, cfg Config, digest string) ([]Candidate, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://127.0.0.1:11434/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "qwen3:4b"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	reqBody := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": extractPrompt},
			{"role": "user", "content": digest},
		},
		"temperature": 0.1,
	}
	buf, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return nil, fmt.Errorf("extract: model returned %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("extract: bad response: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("extract: empty choices")
	}
	return parseCandidates(out.Choices[0].Message.Content)
}

// parseCandidates accepts a bare JSON array or one wrapped in prose/fences.
func parseCandidates(text string) ([]Candidate, error) {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end <= start {
		return nil, nil // model found nothing or returned junk — treat as empty
	}
	var cands []Candidate
	if err := json.Unmarshal([]byte(text[start:end+1]), &cands); err != nil {
		return nil, fmt.Errorf("extract: candidates not parseable: %w", err)
	}
	var kept []Candidate
	for _, c := range cands {
		if c.Content == "" || !c.Type.Valid() {
			continue
		}
		if !secrets.Clean(c.Content) {
			continue // refuse to even propose secrets
		}
		kept = append(kept, c)
	}
	return kept, nil
}

// ProcessSpool handles one spool file: digest, extract, insert as
// trust=extracted (status proposed), then mark it done.
func ProcessSpool(ctx context.Context, db DB, cfg Config, spoolPath string) (int, error) {
	data, err := os.ReadFile(spoolPath)
	if err != nil {
		return 0, err
	}
	var sp Spool
	if err := json.Unmarshal(data, &sp); err != nil {
		return 0, fmt.Errorf("extract: bad spool %s: %w", spoolPath, err)
	}
	digest, err := DigestTranscript(sp.TranscriptPath)
	if err != nil {
		return 0, fmt.Errorf("extract: transcript: %w", err)
	}
	if digest == "" {
		return 0, markDone(spoolPath)
	}
	cands, err := Extract(ctx, cfg, digest)
	if err != nil {
		return 0, err // keep spool pending for retry
	}
	n := 0
	for _, c := range cands {
		m := &memory.Memory{
			Type:        c.Type,
			Content:     c.Content,
			ProjectKey:  sp.ProjectKey,
			SourceAgent: sp.Agent,
			Trust:       memory.TrustExtracted,
			Status:      review.DecideStatus(memory.TrustExtracted),
		}
		if err := db.Insert(m); err != nil {
			if errors.Is(err, store.ErrSecretDetected) {
				continue
			}
			return n, err
		}
		n++
	}
	return n, markDone(spoolPath)
}

// DB is the store surface the worker needs (keeps tests light).
type DB interface {
	Insert(m *memory.Memory) error
}

func markDone(spoolPath string) error {
	return os.Rename(spoolPath, spoolPath+".done")
}

// Run walks the spool directory, oldest first, and processes each file.
// Model unreachability is not fatal: it stops the run quietly (exit 0) so
// hosts calling amh extract are never blocked; spool files stay pending.
func Run(ctx context.Context, db DB, cfg Config, spoolDir string, dryRun bool) (processed, inserted int, err error) {
	entries, err := os.ReadDir(spoolDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		files = append(files, filepath.Join(spoolDir, e.Name()))
	}
	for _, f := range files {
		if dryRun {
			data, rerr := os.ReadFile(f)
			if rerr != nil {
				continue
			}
			var sp Spool
			if json.Unmarshal(data, &sp) == nil {
				fmt.Printf("would process: %s agent=%s project=%s transcript=%s\n",
					filepath.Base(f), sp.Agent, sp.ProjectKey, sp.TranscriptPath)
			}
			processed++
			continue
		}
		n, perr := ProcessSpool(ctx, db, cfg, f)
		processed++
		inserted += n
		if perr != nil {
			if errors.Is(perr, errUnreachable) {
				return processed, inserted, nil // degrade quietly; retry later
			}
			return processed, inserted, perr
		}
	}
	return processed, inserted, nil
}
