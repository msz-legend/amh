package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"amh/internal/consolidate"
)

// runConsolidate implements F1: detect duplicates/contradictions among active
// memories and write a proposal report under <home>/proposals. It never
// mutates memory data.
func runConsolidate(args []string) error {
	fs := flag.NewFlagSet("consolidate", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "print report to stdout without writing a file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	db, _, closeStore, err := openStore()
	if err != nil {
		return err
	}
	defer closeStore()

	rep, err := consolidate.Run(db)
	if err != nil {
		return err
	}
	out := rep.Render()
	if *dryRun {
		fmt.Print(out)
		return nil
	}
	dir := filepath.Join(amhHome(), "proposals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := "consolidate-" + time.Now().Format("20060102-150405") + ".md"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return err
	}
	fmt.Printf("amh consolidate: %d suggestion(s) over %d memories → %s (nothing changed)\n",
		len(rep.Suggestions), rep.Scanned, path)
	return nil
}
