// Package store implements the rebuildable SQLite operational index.
// The index is a projection of the canonical project files; deleting it
// must never destroy user data.
package store

import (
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps the SQLite index database.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
	project_id TEXT PRIMARY KEY,
	title      TEXT NOT NULL,
	language   TEXT NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS paragraphs_fts USING fts5(id UNINDEXED, chapter_id UNINDEXED, text);
CREATE VIRTUAL TABLE IF NOT EXISTS scene_cards_fts USING fts5(scene_id UNINDEXED, title, summary);
CREATE TABLE IF NOT EXISTS imports (
	run_id      TEXT PRIMARY KEY,
	type        TEXT NOT NULL,
	source_path TEXT NOT NULL,
	imported_at TEXT NOT NULL,
	chapters    INTEGER NOT NULL,
	paragraphs  INTEGER NOT NULL,
	status      TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS chapters (
	id         TEXT PRIMARY KEY,
	ordinal    INTEGER NOT NULL UNIQUE,
	title      TEXT NOT NULL,
	file       TEXT NOT NULL,
	source_key TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS blocks (
	chapter_id   TEXT NOT NULL REFERENCES chapters(id),
	ordinal      INTEGER NOT NULL,
	block_type   TEXT NOT NULL,
	paragraph_id TEXT,
	PRIMARY KEY (chapter_id, ordinal)
);
CREATE TABLE IF NOT EXISTS paragraphs (
	id                TEXT PRIMARY KEY,
	chapter_id        TEXT NOT NULL REFERENCES chapters(id),
	ordinal           INTEGER NOT NULL,
	block_type        TEXT NOT NULL,
	text              TEXT NOT NULL,
	text_hash         TEXT NOT NULL,
	source_file       TEXT NOT NULL,
	source_line_start INTEGER NOT NULL,
	source_line_end   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS paragraphs_chapter ON paragraphs(chapter_id, ordinal);
CREATE TABLE IF NOT EXISTS scenes (
	id               TEXT PRIMARY KEY,
	chapter_id       TEXT NOT NULL REFERENCES chapters(id),
	paragraph_start  TEXT NOT NULL REFERENCES paragraphs(id),
	paragraph_end    TEXT NOT NULL REFERENCES paragraphs(id),
	ordinal          INTEGER NOT NULL,
	boundary_source  TEXT NOT NULL,
	status           TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS scenes_chapter ON scenes(chapter_id, ordinal);
CREATE TABLE IF NOT EXISTS scene_cards (
	scene_id         TEXT PRIMARY KEY REFERENCES scenes(id),
	title            TEXT NOT NULL,
	summary          TEXT NOT NULL,
	evidence_json    TEXT NOT NULL,
	generation_run   TEXT NOT NULL,
	generation_model TEXT NOT NULL,
	prompt_version   TEXT NOT NULL,
	status           TEXT NOT NULL,
	raw_json         TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS entities (
	id               TEXT PRIMARY KEY,
	chapter_id       TEXT NOT NULL REFERENCES chapters(id),
	type             TEXT NOT NULL,
	canonical_name   TEXT NOT NULL,
	aliases_json     TEXT NOT NULL,
	evidence_json    TEXT NOT NULL,
	generation_run   TEXT NOT NULL,
	generation_model TEXT NOT NULL,
	prompt_version   TEXT NOT NULL,
	status           TEXT NOT NULL,
	raw_json         TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS occurrences (
	entity_id          TEXT NOT NULL REFERENCES entities(id),
	chapter_id         TEXT NOT NULL REFERENCES chapters(id),
	scene_id           TEXT NOT NULL REFERENCES scenes(id),
	surface_texts_json TEXT NOT NULL,
	source_fields_json TEXT NOT NULL,
	confidence         REAL NOT NULL,
	generation_run     TEXT NOT NULL,
	generation_model   TEXT NOT NULL,
	prompt_version     TEXT NOT NULL,
	status             TEXT NOT NULL,
	raw_json           TEXT NOT NULL,
	PRIMARY KEY (entity_id, scene_id)
);
CREATE INDEX IF NOT EXISTS occurrences_chapter ON occurrences(chapter_id);
CREATE INDEX IF NOT EXISTS occurrences_scene ON occurrences(scene_id);
CREATE TABLE IF NOT EXISTS chapter_entity_snapshots (
	chapter_id       TEXT PRIMARY KEY REFERENCES chapters(id),
	entity_count     INTEGER NOT NULL,
	occurrence_count INTEGER NOT NULL,
	committed_at     TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS model_runs (
	run_id      TEXT PRIMARY KEY,
	run_type    TEXT NOT NULL,
	started_at  TEXT NOT NULL,
	finished_at TEXT,
	status      TEXT NOT NULL,
	model       TEXT NOT NULL,
	prompt_ver  TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS chapter_scene_snapshots (
	chapter_id   TEXT PRIMARY KEY REFERENCES chapters(id),
	committed_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS reverse_index_terms (
	term_type        TEXT NOT NULL,
	term             TEXT NOT NULL,
	occurrence_count INTEGER NOT NULL,
	PRIMARY KEY (term_type, term)
);
CREATE TABLE IF NOT EXISTS reverse_index_refs (
	term_type    TEXT NOT NULL,
	term         TEXT NOT NULL,
	scene_id     TEXT NOT NULL REFERENCES scenes(id),
	chapter_id   TEXT NOT NULL REFERENCES chapters(id),
	source_field TEXT NOT NULL,
	weight       REAL NOT NULL,
	raw_value    TEXT NOT NULL,
	PRIMARY KEY (term_type, term, scene_id, source_field, raw_value)
);
CREATE INDEX IF NOT EXISTS reverse_index_refs_scene ON reverse_index_refs(scene_id);
CREATE INDEX IF NOT EXISTS reverse_index_refs_chapter ON reverse_index_refs(chapter_id);
`

// Open opens (creating if necessary) the SQLite index at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open index %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys for index %s: %w", path, err)
	}
	var foreignKeys int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		db.Close()
		return nil, fmt.Errorf("verify foreign keys for index %s: %w", path, err)
	}
	if foreignKeys != 1 {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys for index %s: pragma remained disabled", path)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize index %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }
