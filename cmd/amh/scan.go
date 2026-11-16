package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"amh/internal/extract"
	"amh/internal/memory"
	"amh/internal/review"
	"amh/internal/scan"
)

func runScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	agent := fs.String("agent", "claude-code", "agent to import from (claude-code|codex)")
	limit := fs.Int("limit", 5, "max recent transcripts to import")
	project := fs.String("project", "", "project key override (default: normalized cwd)")
	dryRun := fs.Bool("dry-run", false, "list transcripts without calling the model")
	timeoutSec := fs.Int("timeout", 120, "per-transcript model timeout in seconds")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	homeUser, _ := os.UserHomeDir()
	ts, err := scan.RecentTranscripts(homeUser, *agent, *limit)
	if err != nil {
		return err
	}
	if len(ts) == 0 {
		fmt.Printf("no %s transcripts found\n", *agent)
		return nil
	}

	projectKey := *project
	if projectKey == "" {
		projectKey = os.Getenv("AMH_PROJECT")
	}
	if projectKey == "" {
		projectKey = "imported"
	}

	db, _, closeDB, err := openStore()
	if err != nil {
		return err
	}
	defer closeDB()

	cfg := extract.Config{
		BaseURL: os.Getenv("AMH_EXTRACT_BASE_URL"),
		APIKey:  os.Getenv("AMH_EXTRACT_API_KEY"),
		Model:   os.Getenv("AMH_EXTRACT_MODEL"),
		Timeout: time.Duration(*timeoutSec) * time.Second,
	}

	total := 0
	for _, t := range ts {
		if *dryRun {
			fmt.Printf("would import: %s (mod %s)\n", t.Path, t.ModTime.Format("2006-01-02 15:04"))
			continue
		}
		digest, derr := scan.DigestTranscript(t.Agent, t.Path)
		if derr != nil || digest == "" {
			mcpLogf("scan: skip %s (%v)", filepath.Base(t.Path), derr)
			continue
		}
		cands, xerr := extract.Extract(context.Background(), cfg, digest)
		if xerr != nil {
			mcpLogf("scan: model failed on %s: %v", filepath.Base(t.Path), xerr)
			continue
		}
		n := 0
		for _, c := range cands {
			m := &memory.Memory{
				Type:        c.Type,
				Content:     c.Content,
				ProjectKey:  projectKey,
				SourceAgent: t.Agent,
				Trust:       memory.TrustExtracted,
				Status:      review.DecideStatus(memory.TrustExtracted),
			}
			if err := db.Insert(m); err != nil {
				continue // secret-filtered or invalid candidates are skipped
			}
			n++
			total++
		}
		fmt.Printf("imported %d memories from %s\n", n, filepath.Base(t.Path))
	}
	if !*dryRun && total > 0 {
		fmt.Printf("cold-start import: %d proposed memories total (run `amh review` to approve)\n", total)
	}
	return nil
}
