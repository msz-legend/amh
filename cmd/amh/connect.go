package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"amh/internal/connect"
)

// runConnect implements `amh connect`: register the amh MCP server into the
// chosen agent host's config. Writes are backed up; --dry-run only prints.
func runConnect(args []string) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	agent := fs.String("agent", "", "target host: claude-code | codex")
	dryRun := fs.Bool("dry-run", false, "show what would be written, change nothing")
	home := fs.String("home", "", "override OS home dir (testing)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *agent != "claude-code" && *agent != "codex" {
		return fmt.Errorf("--agent must be claude-code or codex (got %q)", *agent)
	}
	h := *home
	if h == "" {
		var err error
		h, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home: %w", err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve amh binary path: %w", err)
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}

	entry := connect.Entry{
		Agent:   *agent,
		Command: exe,
		Env: map[string]string{
			"AMH_HOME":  amhHome(),
			"AMH_AGENT": *agent,
		},
	}

	var all []connect.Target
	for _, t := range connect.DefaultTargets(h) {
		if t.Format == "json" && *agent == "claude-code" {
			all = append(all, t)
		}
		if t.Format == "toml" && *agent == "codex" {
			all = append(all, t)
		}
	}
	connect.Inspect(all, "amh")

	for _, t := range all {
		if t.Current != "" {
			mcpLogf("%s: %s (will be replaced)", t.Path, t.Current)
		} else if t.Exists {
			mcpLogf("%s: exists, no amh entry yet (will be added)", t.Path)
		} else {
			mcpLogf("%s: missing (will be created)", t.Path)
		}
	}

	if *dryRun {
		fmt.Println("--- dry run ---")
		fmt.Println(connect.Plan(entry))
		return nil
	}

	for _, t := range all {
		if err := connect.Apply(t, entry, "amh"); err != nil {
			return fmt.Errorf("write %s: %w", t.Path, err)
		}
		mcpLogf("wrote %s (prior file backed up)", t.Path)
	}
	// Claude Code additionally gets session hooks (start injection / end capture).
	if *agent == "claude-code" {
		settings := filepath.Join(h, ".claude", "settings.json")
		if err := connect.ApplyHooks(settings, exe); err != nil {
			return fmt.Errorf("write hooks into %s: %w", settings, err)
		}
		mcpLogf("wrote session hooks into %s", settings)
	}
	fmt.Printf("connected: amh MCP server registered for %s\n", *agent)
	return nil
}
