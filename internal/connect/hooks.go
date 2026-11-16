package connect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// hooksForClaude builds the hooks JSON structure referencing the amh binary.
// On Windows the command avoids cmd quoting issues by using the exe path
// directly (Claude Code runs hook commands via shell, so forward slashes are
// safer than backslashes).
func hookCommand(exe string) string {
	if runtime.GOOS == "windows" {
		return `"` + exe + `" hook %EVENT% --agent claude-code`
	}
	return exe + " hook %EVENT% --agent claude-code"
}

// ApplyHooks merges amh session hooks into Claude Code's settings.json.
// Existing hooks entries (user's own) are preserved.
func ApplyHooks(settingsPath, exe string) error {
	var doc map[string]any
	if data, err := os.ReadFile(settingsPath); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("%s: not valid JSON: %w", settingsPath, err)
		}
	}
	if doc == nil {
		doc = map[string]any{}
	}

	startCmd := replaceEvent(hookCommand(exe), "session-start")
	endCmd := replaceEvent(hookCommand(exe), "session-end")

	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	hooks["SessionStart"] = matchers([]string{startCmd}, hooks["SessionStart"])
	hooks["SessionEnd"] = matchers([]string{endCmd}, hooks["SessionEnd"])
	doc["hooks"] = hooks

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if _, err := backup(settingsPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath, out, 0o644)
}

// matchers keeps any pre-existing matcher groups and appends one amh group.
func matchers(cmds []string, existing any) []map[string]any {
	groups, _ := existing.([]any)
	out := make([]map[string]any, 0, len(groups)+1)
	for _, g := range groups {
		if m, ok := g.(map[string]any); ok {
			out = append(out, m)
		}
	}
	hookList := make([]map[string]any, 0, len(cmds))
	for _, c := range cmds {
		hookList = append(hookList, map[string]any{"type": "command", "command": c})
	}
	out = append(out, map[string]any{"hooks": hookList})
	return out
}

func replaceEvent(cmd, event string) string {
	// %EVENT% placeholder is per-command; each event gets its own literal.
	out := ""
	for i := 0; i < len(cmd); i++ {
		if cmd[i] == '%' && len(cmd)-i >= 7 && cmd[i:i+7] == "%EVENT%" {
			out += event
			i += 6
			continue
		}
		out += string(cmd[i])
	}
	return out
}
