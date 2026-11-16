package extract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amh/internal/memory"
	"amh/internal/secrets"
	"amh/internal/store"
)

func fakeModel(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": reply}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeSpool(t *testing.T, dir, transcript string) string {
	t.Helper()
	tp := filepath.Join(dir, "transcript.jsonl")
	os.WriteFile(tp, []byte(transcript), 0o644)
	sp := Spool{Schema: "amh.spool.v1", Agent: "codex", ProjectKey: "a/repo",
		TranscriptPath: tp, SessionID: "s1", SpooledAt: "t"}
	b, _ := json.Marshal(sp)
	path := filepath.Join(dir, "spool", "20260101T000000-codex-s1.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, b, 0o644)
	return path
}

const sampleTranscript = `{"type":"user","message":{"role":"user","content":"we decided to use SQLite for storage"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"noted: SQLite chosen"}]}}
{"type":"cost-state","message":null}
garbage line
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"gotcha: the API rate-limits after 10 req/s"}]}}`

func TestDigestTranscript(t *testing.T) {
	d := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(d, []byte(sampleTranscript), 0o644)
	digest, err := DigestTranscript(d)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if !strings.Contains(digest, "SQLite for storage") || !strings.Contains(digest, "rate-limits") {
		t.Errorf("digest missing content:\n%s", digest)
	}
	if strings.Contains(digest, "cost-state") || strings.Contains(digest, "garbage") {
		t.Error("digest includes non-message lines")
	}
}

func TestParseCandidates(t *testing.T) {
	in := "Here are the memories:\n```json\n[{\"type\":\"decision\",\"content\":\"use SQLite\"},{\"type\":\"bogus\",\"content\":\"x\"},{\"type\":\"note\",\"content\":\"\"}]\n```"
	c, err := parseCandidates(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(c) != 1 || c[0].Content != "use SQLite" {
		t.Errorf("candidates = %+v", c)
	}
	// Secret candidates are dropped before insertion.
	s := "{\"type\":\"note\",\"content\":\"the key is sk-proj-abcdefghij1234567890\"}"
	if !secrets.Clean(strings.ReplaceAll(s, "\\\"", "\"")) {
		// sanity: the raw secret is detected
	} else {
		t.Fatal("sanity: secret sample should be dirty")
	}
	c2, err := parseCandidates("[" + s + "]")
	if err != nil {
		t.Fatalf("parse2: %v", err)
	}
	if len(c2) != 0 {
		t.Errorf("secret candidate not filtered: %+v", c2)
	}
}

func TestProcessSpoolInsertsProposed(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.MigrateUp()
	path := writeSpool(t, dir, sampleTranscript)
	srv := fakeModel(t, "[{\"type\":\"decision\",\"content\":\"team uses SQLite\"}]")
	n, err := ProcessSpool(context.Background(), db, Config{BaseURL: srv.URL}, path)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if n != 1 {
		t.Fatalf("inserted %d", n)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("spool file not marked done")
	}
	if _, err := os.Stat(path + ".done"); err != nil {
		t.Error("done marker missing")
	}
	all, _ := db.List(memory.StatusProposed, "", 10)
	if len(all) != 1 || all[0].SourceAgent != "codex" || all[0].ProjectKey != "a/repo" ||
		all[0].Trust != memory.TrustExtracted {
		t.Errorf("stored memory wrong: %+v", all)
	}
}

func TestRunUnreachableDegradesQuietly(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.MigrateUp()
	path := writeSpool(t, dir, sampleTranscript)
	// Port 1 is closed.
	p, i, err := Run(context.Background(), db, Config{BaseURL: "http://127.0.0.1:1", Timeout: 300 * time.Millisecond}, filepath.Join(dir, "spool"), false)
	if err != nil {
		t.Fatalf("run should degrade quietly, got: %v", err)
	}
	if p != 1 || i != 0 {
		t.Errorf("processed=%d inserted=%d", p, i)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("spool file should remain pending after unreachable model")
	}
}

func TestRunDryRun(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.MigrateUp()
	writeSpool(t, dir, sampleTranscript)
	p, i, err := Run(context.Background(), db, Config{}, filepath.Join(dir, "spool"), true)
	if err != nil || p != 1 || i != 0 {
		t.Errorf("dry run: p=%d i=%d err=%v", p, i, err)
	}
}
