package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"amh/internal/review"
	"amh/internal/store"
)

func runReview(args []string) error {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: amh review [list|approve <id>|reject <id>]")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	sub := "list"
	if fs.NArg() > 0 {
		sub = fs.Arg(0)
	}

	db, mirrorDir, closeDB, err := openStore()
	if err != nil {
		return err
	}
	defer closeDB()

	switch sub {
	case "list":
		all, err := review.List(db, 100)
		if err != nil {
			return err
		}
		if len(all) == 0 {
			fmt.Println("no proposed memories pending review")
			return nil
		}
		for _, m := range all {
			fmt.Printf("%s  [%s/%s] %s  (source=%s)\n  %s\n",
				m.ID, m.Type, m.Trust, m.CreatedAt.Format("2006-01-02 15:04"),
				m.SourceAgent, firstLine(m.Content))
		}
		return nil
	case "approve":
		if fs.NArg() < 2 {
			return fmt.Errorf("approve needs a memory id")
		}
		m, err := review.Approve(db, fs.Arg(1), mirrorDir)
		if err != nil {
			return err
		}
		fmt.Printf("approved %s (trust=%s status=%s)\n", m.ID, m.Trust, m.Status)
		return nil
	case "reject":
		if fs.NArg() < 2 {
			return fmt.Errorf("reject needs a memory id")
		}
		m, err := review.Reject(db, fs.Arg(1))
		if err != nil {
			return err
		}
		fmt.Printf("rejected %s (status=%s)\n", m.ID, m.Status)
		return nil
	default:
		return fmt.Errorf("unknown review subcommand %q (want list|approve|reject)", sub)
	}
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// openStore opens the DB plus mirror dir from standard env config.
func openStore() (*store.DB, string, func(), error) {
	home := amhHome()
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, "", nil, err
	}
	db, err := store.Open(filepath.Join(home, "amh.db"))
	if err != nil {
		return nil, "", nil, err
	}
	if err := db.MigrateUp(); err != nil {
		db.Close()
		return nil, "", nil, err
	}
	return db, filepath.Join(home, "memories"), func() { db.Close() }, nil
}
