// Package store - test utilities.
//
// This file contains exported helper methods for test use outside the store
// package (e.g. internal/retrieval and internal/query tests). They are named
// with the ForTest suffix to signal test-only intent.
package store

// InsertChapterForTest inserts a minimal chapter row for testing.
func (s *Store) InsertChapterForTest(id string, ordinal int, title string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO chapters (id, ordinal, title, file, source_key) VALUES (?, ?, ?, ?, ?)`,
		id, ordinal, title, "chapters/"+id+".md", id,
	)
	return err
}

// InsertParagraphWithTextForTest inserts a paragraph row with specified text
// and syncs the FTS index.
func (s *Store) InsertParagraphWithTextForTest(id, chapterID string, ordinal int, text string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO paragraphs
		 (id, chapter_id, ordinal, block_type, text, text_hash, source_file, source_line_start, source_line_end)
		 VALUES (?, ?, ?, 'paragraph', ?, 'sha256:test', 'file', 1, 1)`,
		id, chapterID, ordinal, text,
	)
	if err != nil {
		return err
	}
	// Sync FTS: delete any stale entry then insert the new one.
	s.db.Exec(`DELETE FROM paragraphs_fts WHERE id = ?`, id) //nolint:errcheck
	_, err = s.db.Exec(
		`INSERT INTO paragraphs_fts(id, chapter_id, text) VALUES (?, ?, ?)`,
		id, chapterID, text,
	)
	return err
}
