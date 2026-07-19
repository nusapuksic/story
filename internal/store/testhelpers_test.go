package store

// InsertChapterForTest inserts a minimal chapter row for testing.
// It is only available during test builds.
func (s *Store) InsertChapterForTest(id string, ordinal int, title string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO chapters (id, ordinal, title, file, source_key) VALUES (?, ?, ?, ?, ?)`,
		id, ordinal, title, "chapters/"+id+".md", id,
	)
	return err
}

// InsertParagraphForTest inserts a minimal paragraph row for testing.
func (s *Store) InsertParagraphForTest(id, chapterID string, ordinal int) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO paragraphs
		 (id, chapter_id, ordinal, block_type, text, text_hash, source_file, source_line_start, source_line_end)
		 VALUES (?, ?, ?, 'paragraph', 'text', 'sha256:test', 'file', 1, 1)`,
		id, chapterID, ordinal,
	)
	return err
}

// AllScenesForTest returns all scenes and scene cards; used by integration tests.
func AllScenesForTest(s *Store) ([]SceneRow, []SceneCardRow, error) {
	scenes, err := s.AllScenes()
	if err != nil {
		return nil, nil, err
	}
	var cards []SceneCardRow
	for _, sc := range scenes {
		card, err := s.InspectSceneCard(sc.ID)
		if err == nil {
			cards = append(cards, card)
		}
	}
	return scenes, cards, nil
}
