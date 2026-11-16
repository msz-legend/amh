// Package connect wires amh into agent hosts by editing their MCP configs:
//
//   - Claude Code: ~/.claude.json (global) — adds mcpServers.amh (stdio)
//   - Codex:       ~/.codex/config.toml    — adds [mcp_servers.amh]
//
// Every write is preceded by a timestamped backup of the target file and
// supports --dry-run (Plan-only). Existing unrelated keys/sections are
// preserved; an amh entry from a previous connect is replaced in place.
package connect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry describes the amh MCP server registration for one agent.
type Entry struct {
	Agent   string // "claude-code" | "codex"
	Command string // absolute path of the amh binary
	Env     map[string]string
}

// Target is one config file to edit.
type Target struct {
	Path    string
	Format  string // "json" | "toml"
	Scope   string // "global" | "project"
	Exists  bool
	Current string // current amh entry summary ("" = none yet)
}

func claudeJSONPath(home string) string { return filepath.Join(home, ".claude.json") }
func codexTomlPath(home string) string  { return filepath.Join(home, ".codex", "config.toml") }

// DefaultTargets returns the global config targets for both supported hosts.
// home is the OS home directory (passed in for testability).
func DefaultTargets(home string) []Target {
	return []Target{
		{Path: claudeJSONPath(home), Format: "json", Scope: "global"},
		{Path: codexTomlPath(home), Format: "toml", Scope: "global"},
	}
}

// Inspect fills Exists/Current for each target against the given entry name.
func Inspect(targets []Target, name string) {
	for i := range targets {
		data, err := os.ReadFile(targets[i].Path)
		targets[i].Exists = err == nil
		if err != nil {
			continue
		}
		switch targets[i].Format {
		case "json":
			var doc map[string]any
			if json.Unmarshal(data, &doc) == nil {
				if servers, ok := doc["mcpServers"].(map[string]any); ok {
					if _, ok := servers[name]; ok {
						targets[i].Current = "mcpServers." + name + " exists"
					}
				}
			}
		case "toml":
			if hasTOMLTable(string(data), "mcp_servers."+name) {
				targets[i].Current = "[mcp_servers." + name + "] exists"
			}
		}
	}
}

func hasTOMLTable(text, table string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "["+table+"]" {
			return true
		}
	}
	return false
}

// Plan renders the exact new amh entry as it would be written, for --dry-run.
func Plan(e Entry) string {
	switch e.Agent {
	case "claude-code":
		m := map[string]any{"command": e.Command, "args": []string{"serve"}, "env": e.Env}
		b, _ := json.MarshalIndent(m, "  ", "  ")
		return "~/.claude.json -> mcpServers.amh = " + string(b)
	case "codex":
		var b strings.Builder
		b.WriteString("~/.codex/config.toml ->\n[mcp_servers.amh]\n")
		b.WriteString(codexBlock(e))
		return strings.TrimRight(b.String(), "\n")
	default:
		return "unsupported agent " + e.Agent
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ { // tiny n; insertion sort keeps deps zero
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// backup copies path to path.bak-<utc-timestamp> if it exists.
func backup(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	bp := path + ".bak-" + time.Now().UTC().Format("20060102T150405")
	if err := os.WriteFile(bp, data, 0o644); err != nil {
		return "", err
	}
	return bp, nil
}

// Apply writes the amh entry into target. It never touches unrelated content.
func Apply(t Target, e Entry, name string) error {
	data, err := os.ReadFile(t.Path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	switch t.Format {
	case "json":
		var doc map[string]any
		if len(data) > 0 {
			if err := json.Unmarshal(data, &doc); err != nil {
				return fmt.Errorf("%s: not valid JSON: %w", t.Path, err)
			}
		}
		if doc == nil {
			doc = map[string]any{}
		}
		servers, _ := doc["mcpServers"].(map[string]any)
		if servers == nil {
			servers = map[string]any{}
		}
		servers[name] = map[string]any{"command": e.Command, "args": []string{"serve"}, "env": e.Env}
		doc["mcpServers"] = servers
		out, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		out = append(out, '\n')
		if _, err := backup(t.Path); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(t.Path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(t.Path, out, 0o644)
	case "toml":
		text := string(data)
		block := codexBlock(e)
		out := upsertTOMLTable(text, "mcp_servers."+name, block)
		if _, err := backup(t.Path); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(t.Path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(t.Path, []byte(out), 0o644)
	default:
		return fmt.Errorf("unsupported format %q", t.Format)
	}
}

func codexBlock(e Entry) string {
	var b strings.Builder
	b.WriteString("command = \"" + escapeTOML(e.Command) + "\"\n")
	b.WriteString("args = [\"serve\"]\n")
	keys := sortedKeys(e.Env)
	if len(keys) > 0 {
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + " = \"" + escapeTOML(e.Env[k]) + "\""
		}
		b.WriteString("env = { " + strings.Join(parts, ", ") + " }\n")
	}
	return b.String()
}

func escapeTOML(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "\"", "\\\"")
}

// upsertTOMLTable replaces the [table] block (up to the next header) or
// appends it at the end.
func upsertTOMLTable(text, table, block string) string {
	header := "[" + table + "]"
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines)+8)
	skipping := false
	inserted := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if skipping {
			// end of the old block at the next section header
			if strings.HasPrefix(trimmed, "[") {
				skipping = false
				out = append(out, header, strings.TrimRight(block, "\n"), "")
				inserted = true
				out = append(out, line)
				continue
			}
			continue // drop old block lines
		}
		if trimmed == header {
			skipping = true
			continue
		}
		out = append(out, line)
	}
	if skipping && !inserted {
		// table was at EOF
		out = append(out, header, strings.TrimRight(block, "\n"))
		inserted = true
	}
	if !inserted {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, header, strings.TrimRight(block, "\n"))
	}
	return strings.Join(out, "\n")
}
