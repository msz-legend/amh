package eval

import (
	"path/filepath"
	"testing"

	"amh/internal/store"
)

func open(path string) (*store.DB, error) {
	db, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	if err := db.MigrateUp(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func TestRunRecallAt5(t *testing.T) {
	dir := t.TempDir()
	db, err := open(filepath.Join(dir, "eval.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	res, err := Run(db)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Memories != 100 || res.Queries != 30 {
		t.Fatalf("corpus = %d memories, %d queries; want 100/30", res.Memories, res.Queries)
	}
	if res.RecallAt5 < Threshold {
		t.Errorf("recall@5 = %.3f < %.2f; misses: %+v", res.RecallAt5, Threshold, res.Misses)
	}
}

func TestDeterministic(t *testing.T) {
	run := func(name string) float64 {
		dir := t.TempDir()
		db, err := open(filepath.Join(dir, name+".db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		res, err := Run(db)
		if err != nil {
			t.Fatal(err)
		}
		return res.RecallAt5
	}
	r1, r2 := run("a"), run("b")
	if r1 != r2 {
		t.Errorf("nondeterministic recall: %.3f vs %.3f", r1, r2)
	}
}
