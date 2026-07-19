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
`

// Open opens (creating if necessary) the SQLite index at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open index %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize index %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }
