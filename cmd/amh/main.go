package main

import (
	"fmt"
	"os"
	"path/filepath"

	"amh/internal/amhserver"
	"amh/internal/project"
	"amh/internal/store"
)

const version = "0.0.1-dev"

func amhHome() string {
	if h := os.Getenv("AMH_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".amh"
	}
	return filepath.Join(home, ".amh")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println("amh", version)
	case "serve":
		serve()
	case "connect":
		if err := runConnect(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "hook":
		os.Exit(runHook(os.Args[2:]))
	case "review":
		if err := runReview(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "extract":
		if err := runExtract(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "scan":
		if err := runScan(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "ui":
		if err := runUI(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "eval":
		os.Exit(runEval())
	case "consolidate":
		if err := runConsolidate(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "reindex":
		if err := runReindex(os.Args[2:]); err != nil {
			fatal(err)
		}
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `amh — AgentMemory Hub

Usage:
  amh version            print version
  amh serve              run the MCP server over stdio
  amh connect --agent <claude-code|codex> [--dry-run]
                         register the amh MCP server into an agent host
  amh hook session-start|session-end --agent <name>
                         host lifecycle hook (injection / capture)
  amh review [list|approve <id>|reject <id>]
                         human review of proposed (extracted) memories
  amh extract [--dry-run]
                         turn session-end spools into proposed memories via
                         an external OpenAI-compatible API
                         (AMH_EXTRACT_BASE_URL/API_KEY/MODEL required)
  amh scan --agent <claude-code|codex> [--limit N] [--dry-run]
                         cold-start import of historical sessions
  amh ui [--addr 127.0.0.1:7788]
                         localhost web UI to review/edit/sync memories
  amh eval               run the E1 retrieval benchmark (recall@5 ≥ 0.85)
  amh consolidate [--dry-run]
                         propose duplicate/contradiction cleanup (report only)
  amh reindex [--prune]  rebuild the index from Markdown cards (git sync)

Environment:
  AMH_HOME     data root (default ~/.amh)
  AMH_AGENT    source agent identity, e.g. claude-code | codex
  AMH_PROJECT  default normalized project key`)
}

func serve() {
	home := amhHome()
	dbPath := os.Getenv("AMH_DB")
	if dbPath == "" {
		dbPath = filepath.Join(home, "amh.db")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		fatal(err)
	}
	if err := db.MigrateUp(); err != nil {
		fatal(err)
	}
	mirrorDir := filepath.Join(home, "memories")
	projectKey := os.Getenv("AMH_PROJECT")
	if projectKey == "" {
		// Fall back to normalizing the git remote of the working directory;
		// last resort is the escaped absolute cwd (project.EscapePath).
		projectKey = project.ResolveDir(".")
	}
	cfg := amhserver.Config{
		DB:        db,
		MirrorDir: mirrorDir,
		Agent:     os.Getenv("AMH_AGENT"),
		Project:   projectKey,
	}
	// Identity is required: refuse to serve unattributed writes.
	if cfg.Agent == "" {
		fatal(fmt.Errorf("AMH_AGENT must be set (e.g. claude-code or codex); unattributed memories are rejected"))
	}
	mcpLogf("amh serve: db=%s agent=%s project=%s", dbPath, cfg.Agent, cfg.Project)
	if err := amhserver.Server(cfg).Serve(os.Stdin, os.Stdout); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "amh:", err)
	os.Exit(1)
}

// mcpLogf goes to stderr; keep stdout clean for the protocol.
func mcpLogf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "amh: "+format+"\n", args...)
}
