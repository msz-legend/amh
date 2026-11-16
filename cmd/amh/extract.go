package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"amh/internal/extract"
)

func runExtract(args []string) error {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "list pending spool files without calling the model")
	timeout := fs.Duration("timeout", 60*time.Second, "per-call model timeout")
	if err := fs.Parse(args); err != nil {
		return err
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
		Timeout: *timeout,
	}
	sp := filepath.Join(amhHome(), "spool")
	if *dryRun {
		_, _, err = extract.Run(context.Background(), db, cfg, sp, true)
		return err
	}
	processed, inserted, err := extract.Run(context.Background(), db, cfg, sp, false)
	mcpLogf("extract: processed=%d inserted=%d", processed, inserted)
	if inserted > 0 {
		fmt.Printf("extracted %d candidate memories -> proposed (run `amh review` to approve)\n", inserted)
	}
	return err
}
