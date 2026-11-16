package main

import (
	"flag"
	"fmt"
	"os"

	"amh/internal/reindex"
)

// runReindex implements F2: rebuild the serving SQLite index from the
// Markdown card tree (the git-synced source of truth).
func runReindex(args []string) error {
	fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
	prune := fs.Bool("prune", false, "delete index rows that have no card (after pulling cards from git)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	db, mirrorDir, closeStore, err := openStore()
	if err != nil {
		return err
	}
	defer closeStore()
	res, err := reindex.Run(db, mirrorDir, *prune)
	if err != nil {
		return err
	}
	fmt.Printf("amh reindex: cards=%d upserted=%d skipped=%d pruned=%d\n",
		res.Cards, res.Upserted, res.Skipped, res.Pruned)
	for _, e := range res.Errors {
		fmt.Fprintln(os.Stderr, "  "+e)
	}
	return nil
}
