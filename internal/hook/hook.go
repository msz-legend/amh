// Package hook implements the session-start injection and session-end
// capture adapters (PRD §6 M3). Hooks NEVER block or fail the host agent:
// on any error the process exits 0 with empty stdout, logging to stderr.
package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"amh/internal/amhserver"
	"amh/internal/project"
	"amh/internal/store"
)

// Payload mirrors the Claude Code hook stdin JSON (SessionStart and
// SessionEnd share the core fields).
type Payload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Source         string `json:"source"`
	Reason         string `json:"reason"`
}

// Options carry what hooks need beyond the payload.
type Options struct {
	Home      string // AMH_HOME data root
	Agent     string // e.g. "claude-code"; identity comes from CLI/env, never the model
	Limit     int    // injection size cap
	NowFn     func() time.Time
	ResolveFn func(dir string) string // project resolution; injectable for tests
}

// SessionStartContext renders the injection context for a session start.
func SessionStartContext(p Payload, o Options) (string, error) {
	if o.Agent == "" {
		return "", fmt.Errorf("agent identity required")
	}
	dbPath := filepath.Join(o.Home, "amh.db")
	db, err := store.Open(dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()
	if err := db.MigrateUp(); err != nil {
		return "", err
	}
	proj := resolveProject(p, o)
	cfg := amhserver.Config{DB: db, Agent: o.Agent, Project: proj}
	return cfg.ResumeText(proj, o.Limit), nil
}

// SessionStartOutput wraps the context in Claude Code's hook response JSON.
func SessionStartOutput(context string) ([]byte, error) {
	resp := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": context,
		},
	}
	return json.Marshal(resp)
}

// SessionEndSpool persists the session-end payload to <home>/spool for the
// extraction worker (D2) to pick up later. Returns the spool file path.
func SessionEndSpool(p Payload, o Options) (string, error) {
	if o.Agent == "" {
		return "", fmt.Errorf("agent identity required")
	}
	dir := filepath.Join(o.Home, "spool")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if o.NowFn != nil {
		now = o.NowFn()
	}
	proj := resolveProject(p, o)
	doc := map[string]any{
		"schema":          "amh.spool.v1",
		"captured_at":     now.Format(time.RFC3339Nano),
		"agent":           o.Agent,
		"session_id":      p.SessionID,
		"transcript_path": p.TranscriptPath,
		"cwd":             p.Cwd,
		"project_key":     proj,
		"hook_event":      p.HookEventName,
		"reason":          p.Reason,
	}
	name := fmt.Sprintf("%s-%s-%s.json", now.Format("20060102T150405"), o.Agent, shortID(p.SessionID))
	path := filepath.Join(dir, name)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func resolveProject(p Payload, o Options) string {
	if o.ResolveFn != nil {
		return o.ResolveFn(p.Cwd)
	}
	return project.ResolveDir(p.Cwd)
}

func shortID(sid string) string {
	if len(sid) > 8 {
		return sid[:8]
	}
	if sid == "" {
		return "nosession"
	}
	return sid
}
