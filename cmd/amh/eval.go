package main

import (
	"fmt"
	"os"
	"path/filepath"

	"amh/internal/eval"
	"amh/internal/store"
)

// runEval executes the E1 retrieval benchmark against a throwaway database
// and prints the recall@5 report. Exit code 1 signals a threshold failure.
func runEval() int {
	dir, err := os.MkdirTemp("", "amh-eval-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "amh eval: temp dir:", err)
		return 1
	}
	defer os.RemoveAll(dir)
	db, err := store.Open(filepath.Join(dir, "eval.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "amh eval: open:", err)
		return 1
	}
	defer db.Close()
	if err := db.MigrateUp(); err != nil {
		fmt.Fprintln(os.Stderr, "amh eval: migrate:", err)
		return 1
	}
	res, err := eval.Run(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, "amh eval:", err)
		return 1
	}
	fmt.Printf("amh eval: corpus=%d memories, queries=%d → recall@5=%.3f (threshold %.2f)\n",
		res.Memories, res.Queries, res.RecallAt5, eval.Threshold)
	for _, m := range res.Misses {
		fmt.Printf("  miss: %q recall=%.2f\n", m.Query, m.Recall)
	}
	if !res.Pass() {
		fmt.Fprintln(os.Stderr, "amh eval: FAIL")
		return 1
	}
	fmt.Println("amh eval: PASS")
	return 0
}
