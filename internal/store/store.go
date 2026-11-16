// Package store implements the SQLite-backed persistence layer of amh.
// SQLite is the primary data; a Markdown mirror is layered on top in A3.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, no CGO
)

// DB wraps the sql.DB handle. A single connection is enforced because
// modernc/sqlite is safest with one writer.
type DB struct {
	*sql.DB
}

// Open opens (creating if needed) the amh database.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db}, nil
}

// NewID returns a random 128-bit hex identifier.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return hex.EncodeToString(b)
}

// Time marshals timestamps as RFC3339 UTC strings for stable ordering.
func Time(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// ParseTime reads back what Time wrote.
func ParseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// migration is one ordered schema step.
type migration struct {
	version int
	up      string
	down    string
}

var migrations = []migration{
	{
		version: 1,
		up: `
CREATE TABLE memories (
	id            TEXT PRIMARY KEY,
	type          TEXT NOT NULL CHECK (type IN ('decision','gotcha','preference','project-rule','handoff','note')),
	content       TEXT NOT NULL,
	project_key   TEXT NOT NULL DEFAULT '',
	source_agent  TEXT NOT NULL DEFAULT '',
	trust         TEXT NOT NULL CHECK (trust IN ('explicit','extracted','user')),
	status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','proposed','retired')),
	superseded_by TEXT NOT NULL DEFAULT '',
	first_seen    TEXT NOT NULL,
	created_at    TEXT NOT NULL
);
CREATE INDEX idx_memories_project_status ON memories(project_key, status);
CREATE INDEX idx_memories_type           ON memories(type);
`,
		down: `
DROP INDEX IF EXISTS idx_memories_type;
DROP INDEX IF EXISTS idx_memories_project_status;
DROP TABLE memories;
`,
	},
	// v2: FTS5 full-text index over memories (external-content table, kept in
	// sync by triggers so every write path — store, UI, review — stays correct).
	{
		version: 2,
		up: `
CREATE VIRTUAL TABLE memories_fts USING fts5(
	content, project_key, source_agent, type,
	content='memories', content_rowid='rowid'
);
CREATE TRIGGER memories_fts_ai AFTER INSERT ON memories BEGIN
	INSERT INTO memories_fts(rowid, content, project_key, source_agent, type)
	VALUES (new.rowid, new.content, new.project_key, new.source_agent, new.type);
END;
CREATE TRIGGER memories_fts_ad AFTER DELETE ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, content, project_key, source_agent, type)
	VALUES ('delete', old.rowid, old.content, old.project_key, old.source_agent, old.type);
END;
CREATE TRIGGER memories_fts_au AFTER UPDATE OF content, project_key, source_agent, type ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, content, project_key, source_agent, type)
	VALUES ('delete', old.rowid, old.content, old.project_key, old.source_agent, old.type);
	INSERT INTO memories_fts(rowid, content, project_key, source_agent, type)
	VALUES (new.rowid, new.content, new.project_key, new.source_agent, new.type);
END;
INSERT INTO memories_fts(rowid, content, project_key, source_agent, type)
	SELECT rowid, content, project_key, source_agent, type FROM memories;
`,
		down: `
DROP TRIGGER IF EXISTS memories_fts_au;
DROP TRIGGER IF EXISTS memories_fts_ad;
DROP TRIGGER IF EXISTS memories_fts_ai;
DROP TABLE IF EXISTS memories_fts;
`,
	},
}

// MigrateUp applies all pending migrations.
func (db *DB) MigrateUp() error { return db.runMigrations(true) }

// MigrateDown rolls back the most recent migration.
func (db *DB) MigrateDown() error { return db.runMigrations(false) }

// SchemaVersion reports the current applied schema version.
func (db *DB) SchemaVersion() (int, error) {
	var v int
	err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v)
	return v, err
}

func (db *DB) runMigrations(up bool) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	current, err := db.SchemaVersion()
	if err != nil {
		return err
	}
	for _, m := range migrations {
		switch {
		case up && m.version > current:
			if _, err := db.Exec(m.up); err != nil {
				return fmt.Errorf("migration %d up: %w", m.version, err)
			}
			if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
				m.version, Time(time.Now())); err != nil {
				return err
			}
		case !up && m.version == current:
			if _, err := db.Exec(m.down); err != nil {
				return fmt.Errorf("migration %d down: %w", m.version, err)
			}
			if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = ?`, m.version); err != nil {
				return err
			}
			current--
		}
	}
	return nil
}
