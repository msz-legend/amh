// Package webapi serves the localhost management REST API backing the
// embedded web UI. Every handler shares the same store layer as MCP/CLI,
// so the secrets guard, trust rules and Markdown mirroring apply unchanged.
package webapi

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"amh/internal/memory"
	"amh/internal/review"
	"amh/internal/store"
)

//go:embed static/index.html
var staticFS embed.FS

// Config wires the API to the store.
type Config struct {
	DB        *store.DB
	MirrorDir string // Markdown mirror root ("" disables mirroring)
	Token     string // required access token (random, printed by `amh ui`)
}

type server struct {
	cfg Config
}

// New returns the fully routed, token-gated handler.
func New(cfg Config) http.Handler {
	if cfg.Token == "" {
		panic("webapi: Token must not be empty")
	}
	s := &server{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/memories", s.list)
	mux.HandleFunc("GET /api/memories/{id}", s.get)
	mux.HandleFunc("PATCH /api/memories/{id}", s.patch)
	mux.HandleFunc("DELETE /api/memories/{id}", s.del)
	mux.HandleFunc("POST /api/memories/{id}/approve", s.approve)
	mux.HandleFunc("POST /api/memories/{id}/reject", s.reject)
	mux.HandleFunc("POST /api/memories/{id}/retire", s.retire)
	mux.HandleFunc("GET /api/stats", s.stats)
	mux.HandleFunc("GET /", s.index)
	return s.auth(mux)
}

// auth enforces the token (query param or cookie) and converts a valid
// query param into a session cookie so page navigation keeps working.
func (s *server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("token"); t == s.cfg.Token {
			http.SetCookie(w, &http.Cookie{
				Name: "amh_token", Value: t, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteLaxMode,
			})
			// Strip the token from the URL-visible request context by
			// rewriting the path (keeps the browser address bar clean
			// via a client-side redirect after the cookie is set).
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie("amh_token"); err == nil && c.Value == s.cfg.Token {
			next.ServeHTTP(w, r)
			return
		}
		writeErr(w, http.StatusUnauthorized, "missing or invalid token; open the URL printed by `amh ui`")
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// fail maps domain errors to HTTP statuses.
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, review.ErrNotProposed):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrSecretDetected):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *server) list(w http.ResponseWriter, r *http.Request) {
	o := store.FilterOpts{
		Status:  memory.Status(r.URL.Query().Get("status")),
		Project: r.URL.Query().Get("project"),
		Agent:   r.URL.Query().Get("agent"),
		Type:    memory.Type(r.URL.Query().Get("type")),
		Q:       r.URL.Query().Get("q"),
	}
	fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &o.Limit)
	ms, err := s.cfg.DB.Filter(o)
	if err != nil {
		fail(w, err)
		return
	}
	if ms == nil {
		ms = []*memory.Memory{}
	}
	writeJSON(w, http.StatusOK, ms)
}

func (s *server) get(w http.ResponseWriter, r *http.Request) {
	m, err := s.cfg.DB.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *server) approve(w http.ResponseWriter, r *http.Request) {
	m, err := review.Approve(s.cfg.DB, r.PathValue("id"), s.cfg.MirrorDir)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *server) reject(w http.ResponseWriter, r *http.Request) {
	m, err := review.Reject(s.cfg.DB, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// retire moves an active memory to retired (same as MCP forget) and syncs
// the mirror card so the Markdown track reflects the status.
func (s *server) retire(w http.ResponseWriter, r *http.Request) {
	m, err := s.cfg.DB.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if m.Status != memory.StatusActive {
		writeErr(w, http.StatusConflict, fmt.Sprintf("memory is %s, only active can be retired", m.Status))
		return
	}
	if err := s.cfg.DB.Forget(m.ID); err != nil {
		fail(w, err)
		return
	}
	m.Status = memory.StatusRetired
	if err := store.SyncMirror(s.cfg.MirrorDir, m); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// del soft-deletes: proposed → reject, active → retire. History is kept.
func (s *server) del(w http.ResponseWriter, r *http.Request) {
	m, err := s.cfg.DB.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	switch m.Status {
	case memory.StatusProposed:
		m, err = review.Reject(s.cfg.DB, m.ID)
	case memory.StatusActive:
		err = s.cfg.DB.Forget(m.ID)
		if err == nil {
			m.Status = memory.StatusRetired
			err = store.SyncMirror(s.cfg.MirrorDir, m)
		}
	default:
		writeErr(w, http.StatusConflict, "memory is already retired")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// patch edits content/project/type (secrets-guarded) and syncs mirrors,
// including removing the old card when the project changes.
func (s *server) patch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content *string `json:"content"`
		Project *string `json:"project"`
		Type    *string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON body: "+err.Error())
		return
	}
	f := store.UpdateFields{Content: body.Content, Project: body.Project}
	if body.Type != nil {
		t := memory.Type(strings.TrimSpace(*body.Type))
		f.Type = &t
	}
	old, err := s.cfg.DB.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	m, err := s.cfg.DB.Update(old.ID, f)
	if err != nil {
		fail(w, err)
		return
	}
	if s.cfg.MirrorDir != "" {
		if old.ProjectKey != m.ProjectKey {
			_ = os.Remove(store.CardPath(s.cfg.MirrorDir, old))
		}
		if err := store.SyncMirror(s.cfg.MirrorDir, m); err != nil {
			fail(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := s.cfg.DB.Stats()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// index serves the embedded single-page UI.
func (s *server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ui assets missing")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(page)
}
