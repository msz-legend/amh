package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amh/internal/memory"
	"amh/internal/store"
)

func newTestServer(t *testing.T) (*store.DB, string, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.MigrateUp(); err != nil {
		t.Fatal(err)
	}
	token := "test-token"
	h := New(Config{DB: db, MirrorDir: filepath.Join(dir, "memories"), Token: token})
	return db, token, h
}

// client wraps the handler with token auth, mirroring a real browser session
// (cookie pre-set, as after the first ?token= visit).
type client struct {
	t       *testing.T
	h       http.Handler
	token   string
	cookies []*http.Cookie
}

func newClient(t *testing.T, h http.Handler, token string) *client {
	return &client{t: t, h: h, token: token,
		cookies: []*http.Cookie{{Name: "amh_token", Value: token}}}
}

func (c *client) do(method, path string, body string) (*httptest.ResponseRecorder, map[string]any) {
	c.t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	var out map[string]any
	if len(rec.Body.Bytes()) > 0 && rec.Body.Bytes()[0] == '{' {
		json.Unmarshal(rec.Body.Bytes(), &out) // best-effort; tests parse as needed
	}
	if set := rec.Result().Cookies(); len(set) > 0 {
		c.cookies = append(c.cookies, set...)
	}
	return rec, out
}

func mustInsert(t *testing.T, db *store.DB, typ memory.Type, content, project, agent string, trust memory.Trust, status memory.Status) *memory.Memory {
	t.Helper()
	m := &memory.Memory{Type: typ, Content: content, ProjectKey: project, SourceAgent: agent, Trust: trust, Status: status}
	if err := db.Insert(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAuthRequired(t *testing.T) {
	_, token, h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/memories", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d %s", rec.Code, rec.Body.String())
	}
	// Wrong token.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/memories?token=wrong", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	// Query param grants access and sets cookie.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/memories?token="+token, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("query token: %d %s", rec.Code, rec.Body.String())
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "amh_token" && c.Value == token {
			found = true
		}
	}
	if !found {
		t.Error("token cookie not set")
	}
}

func TestListAndFilters(t *testing.T) {
	db, token, h := newTestServer(t)
	mustInsert(t, db, memory.TypeDecision, "uses SQLite", "a/repo", "codex", memory.TrustExplicit, memory.StatusActive)
	mustInsert(t, db, memory.TypeGotcha, "windows paths", "b/repo", "claude-code", memory.TrustExplicit, memory.StatusActive)
	mustInsert(t, db, memory.TypeNote, "maybe prefers tabs", "a/repo", "codex", memory.TrustExtracted, memory.StatusProposed)
	c := newClient(t, h, token)

	got := func(path string) int {
		rec, _ := c.do("GET", path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		var ms []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &ms); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return len(ms)
	}
	if n := got("/api/memories"); n != 3 {
		t.Errorf("all: %d", n)
	}
	if n := got("/api/memories?status=proposed"); n != 1 {
		t.Errorf("proposed: %d", n)
	}
	if n := got("/api/memories?agent=claude-code"); n != 1 {
		t.Errorf("agent: %d", n)
	}
	if n := got("/api/memories?project=a/repo"); n != 2 {
		t.Errorf("project: %d", n)
	}
	if n := got("/api/memories?type=decision"); n != 1 {
		t.Errorf("type: %d", n)
	}
	if n := got("/api/memories?q=sqlite"); n != 1 {
		t.Errorf("q: %d", n)
	}
}

func TestApproveRejectLifecycle(t *testing.T) {
	db, token, h := newTestServer(t)
	p := mustInsert(t, db, memory.TypeNote, "candidate", "p", "codex", memory.TrustExtracted, memory.StatusProposed)
	a := mustInsert(t, db, memory.TypeDecision, "active one", "p", "codex", memory.TrustExplicit, memory.StatusActive)
	c := newClient(t, h, token)

	rec, _ := c.do("POST", "/api/memories/"+p.ID+"/approve", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := db.Get(p.ID)
	if got.Status != memory.StatusActive || got.Trust != memory.TrustUser {
		t.Errorf("approved memory wrong: %+v", got)
	}
	// Approving again conflicts.
	rec, _ = c.do("POST", "/api/memories/"+p.ID+"/approve", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("re-approve: %d", rec.Code)
	}
	// Reject the active one → conflict.
	rec, _ = c.do("POST", "/api/memories/"+a.ID+"/reject", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("reject active: %d", rec.Code)
	}
	// Retire the active one.
	rec, _ = c.do("POST", "/api/memories/"+a.ID+"/retire", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("retire: %d %s", rec.Code, rec.Body.String())
	}
	got, _ = db.Get(a.ID)
	if got.Status != memory.StatusRetired {
		t.Errorf("retired memory: %+v", got)
	}
	// DELETE a proposed memory retires it.
	p2 := mustInsert(t, db, memory.TypeNote, "junk", "p", "codex", memory.TrustExtracted, memory.StatusProposed)
	rec, _ = c.do("DELETE", "/api/memories/"+p2.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete proposed: %d", rec.Code)
	}
	// Unknown id → 404.
	rec, _ = c.do("GET", "/api/memories/deadbeef", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("404: %d", rec.Code)
	}
}

func TestPatchEditsAndMirror(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.MigrateUp()
	mirror := filepath.Join(dir, "memories")
	m := mustInsert(t, db, memory.TypeDecision, "old content", "old/project", "codex", memory.TrustExplicit, memory.StatusActive)
	c := newClient(t, New(Config{DB: db, MirrorDir: mirror, Token: "t"}), "t")

	body := `{"content":"new content","project":"new/project","type":"gotcha"}`
	rec, _ := c.do("PATCH", "/api/memories/"+m.ID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := db.Get(m.ID)
	if got.Content != "new content" || got.ProjectKey != "new/project" || got.Type != memory.TypeGotcha {
		t.Errorf("patched: %+v", got)
	}
	// New card exists, old card removed.
	if _, err := os.Stat(filepath.Join(mirror, "new-project", m.ID+".md")); err != nil {
		t.Errorf("new card missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mirror, "old-project", m.ID+".md")); err == nil {
		t.Error("old card still present after project change")
	}
	// Secret content is refused.
	rec, _ = c.do("PATCH", "/api/memories/"+m.ID, `{"content":"aws key AKIAIOSFODNN7EXAMPLE"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("secret patch: %d %s", rec.Code, rec.Body.String())
	}
}

func TestStats(t *testing.T) {
	db, token, h := newTestServer(t)
	mustInsert(t, db, memory.TypeDecision, "d1", "p1", "codex", memory.TrustExplicit, memory.StatusActive)
	mustInsert(t, db, memory.TypeNote, "n1", "p2", "claude-code", memory.TrustExtracted, memory.StatusProposed)
	c := newClient(t, h, token)
	rec, out := c.do("GET", "/api/stats", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: %d", rec.Code)
	}
	if int(out["total"].(float64)) != 2 {
		t.Errorf("total: %v", out)
	}
	bs := out["by_status"].(map[string]any)
	if int(bs["active"].(float64)) != 1 || int(bs["proposed"].(float64)) != 1 {
		t.Errorf("by_status: %v", bs)
	}
}

func TestIndexPage(t *testing.T) {
	_, token, h := newTestServer(t)
	c := newClient(t, h, token)
	rec, _ := c.do("GET", "/", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("index: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	// Unknown path → 404.
	rec, _ = c.do("GET", "/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("404 path: %d", rec.Code)
	}
}
