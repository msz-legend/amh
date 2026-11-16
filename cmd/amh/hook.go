package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"amh/internal/hook"
)

// runHook implements `amh hook session-start|session-end` for Claude Code
// hooks. Contract: on ANY error, exit 0 with empty stdout (never block the
// host), diagnostics on stderr.
func runHook(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "amh hook: need session-start or session-end")
		return 1
	}
	fs := flag.NewFlagSet("hook "+args[0], flag.ContinueOnError)
	agent := fs.String("agent", "", "source agent identity (claude-code)")
	limit := fs.Int("limit", 10, "max injected memories")
	home := fs.String("home", "", "override AMH_HOME")
	if err := fs.Parse(args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "amh hook:", err)
		return 1
	}
	h := *home
	if h == "" {
		h = amhHome()
	}
	if *agent == "" {
		*agent = os.Getenv("AMH_AGENT")
	}

	payload := readPayload(os.Stdin)
	opts := hook.Options{Home: h, Agent: *agent, Limit: *limit}

	switch args[0] {
	case "session-start":
		ctx, err := hook.SessionStartContext(payload, opts)
		if err != nil {
			// Hook must never block the session; report and exit cleanly.
			mcpLogf("hook session-start skipped: %v", err)
			return 0
		}
		out, err := hook.SessionStartOutput(ctx)
		if err != nil {
			mcpLogf("hook session-start skipped: %v", err)
			return 0
		}
		os.Stdout.Write(out)
		return 0
	case "session-end":
		path, err := hook.SessionEndSpool(payload, opts)
		if err != nil {
			mcpLogf("hook session-end skipped: %v", err)
			return 0
		}
		mcpLogf("spooled %s", path)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "amh hook: unknown event", args[0])
		return 1
	}
}

// readPayload parses the hook stdin JSON; empty or malformed stdin is
// tolerated (hooks may be invoked manually).
func readPayload(r io.Reader) hook.Payload {
	var p hook.Payload
	data, err := io.ReadAll(r)
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &p) // best effort
	}
	return p
}
