package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// SceneRow is scene metadata read from the index.
type SceneRow struct {
	ID             string
	ChapterID      string
	ParagraphStart string
	ParagraphEnd   string
	Ordinal        int
	BoundarySource string // "explicit", "model", "manual"
	Status         string // "generated", "verified", "accepted", "rejected"
}

// SceneCardRow is a scene card record read from the index.
type SceneCardRow struct {
	SceneID         string
	Title           string
	Summary         string
	Evidence        []string
	GenerationRun   string
	GenerationModel string
	PromptVersion   string
	Status          string
	RawJSON         string
}

// InsertScene inserts one scene row.  Duplicate scene IDs are replaced.
func (s *Store) InsertScene(r SceneRow) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO scenes
			(id, chapter_id, paragraph_start, paragraph_end, ordinal, boundary_source, status)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.ChapterID, r.ParagraphStart, r.ParagraphEnd,
		r.Ordinal, r.BoundarySource, r.Status,
	)
	if err != nil {
		return fmt.Errorf("insert scene %s: %w", r.ID, err)
	}
	return nil
}

// InsertSceneCard inserts or replaces a scene card row.
func (s *Store) InsertSceneCard(r SceneCardRow) error {
	evJSON, err := json.Marshal(r.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence for scene card %s: %w", r.SceneID, err)
	}
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO scene_cards
			(scene_id, title, summary, evidence_json, generation_run, generation_model,
			 prompt_version, status, raw_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.SceneID, r.Title, r.Summary, string(evJSON),
		r.GenerationRun, r.GenerationModel, r.PromptVersion, r.Status, r.RawJSON,
	)
	if err != nil {
		return fmt.Errorf("insert scene card %s: %w", r.SceneID, err)
	}
	// Sync FTS: delete any existing entry then insert the new one.
	s.db.Exec(`DELETE FROM scene_cards_fts WHERE scene_id = ?`, r.SceneID) //nolint:errcheck
	if _, err := s.db.Exec(
		`INSERT INTO scene_cards_fts(scene_id, title, summary) VALUES (?, ?, ?)`,
		r.SceneID, r.Title, r.Summary,
	); err != nil {
		return fmt.Errorf("index scene card FTS %s: %w", r.SceneID, err)
	}
	return nil
}

// ScenesByChapter returns all scenes for a chapter, ordered by ordinal.
func (s *Store) ScenesByChapter(chapterID string) ([]SceneRow, error) {
	rows, err := s.db.Query(
		`SELECT id, chapter_id, paragraph_start, paragraph_end, ordinal, boundary_source, status
		 FROM scenes WHERE chapter_id = ? ORDER BY ordinal`,
		chapterID,
	)
	if err != nil {
		return nil, fmt.Errorf("scenes for chapter %s: %w", chapterID, err)
	}
	defer rows.Close()
	return scanScenes(rows)
}

// AllScenes returns all scenes ordered by chapter ordinal then scene ordinal.
func (s *Store) AllScenes() ([]SceneRow, error) {
	rows, err := s.db.Query(
		`SELECT s.id, s.chapter_id, s.paragraph_start, s.paragraph_end,
		        s.ordinal, s.boundary_source, s.status
		 FROM scenes s
		 JOIN chapters c ON s.chapter_id = c.id
		 ORDER BY c.ordinal, s.ordinal`,
	)
	if err != nil {
		return nil, fmt.Errorf("all scenes: %w", err)
	}
	defer rows.Close()
	return scanScenes(rows)
}

func scanScenes(rows *sql.Rows) ([]SceneRow, error) {
	var out []SceneRow
	for rows.Next() {
		var r SceneRow
		if err := rows.Scan(&r.ID, &r.ChapterID, &r.ParagraphStart, &r.ParagraphEnd,
			&r.Ordinal, &r.BoundarySource, &r.Status); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ParagraphsByChapter returns all paragraph rows for a chapter, ordered by
// ordinal.
func (s *Store) ParagraphsByChapter(chapterID string) ([]ParagraphRow, error) {
	rows, err := s.db.Query(
		`SELECT id, chapter_id, ordinal, block_type, text, text_hash,
		        source_file, source_line_start, source_line_end
		 FROM paragraphs WHERE chapter_id = ? ORDER BY ordinal`,
		chapterID,
	)
	if err != nil {
		return nil, fmt.Errorf("paragraphs for chapter %s: %w", chapterID, err)
	}
	defer rows.Close()
	var out []ParagraphRow
	for rows.Next() {
		var p ParagraphRow
		if err := rows.Scan(&p.ID, &p.ChapterID, &p.Ordinal, &p.BlockType,
			&p.Text, &p.TextHash, &p.SourceFile, &p.SourceLineStart, &p.SourceLineEnd); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AllChapters returns all chapter rows ordered by ordinal.
func (s *Store) AllChapters() ([]ChapterRow, error) {
	rows, err := s.db.Query(
		`SELECT id, ordinal, title, file, source_key FROM chapters ORDER BY ordinal`,
	)
	if err != nil {
		return nil, fmt.Errorf("all chapters: %w", err)
	}
	defer rows.Close()
	var out []ChapterRow
	for rows.Next() {
		var c ChapterRow
		if err := rows.Scan(&c.ID, &c.Ordinal, &c.Title, &c.File, &c.SourceKey); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InspectScene returns a scene by ID.
func (s *Store) InspectScene(id string) (SceneRow, error) {
	var r SceneRow
	err := s.db.QueryRow(
		`SELECT id, chapter_id, paragraph_start, paragraph_end, ordinal, boundary_source, status
		 FROM scenes WHERE id = ?`, id,
	).Scan(&r.ID, &r.ChapterID, &r.ParagraphStart, &r.ParagraphEnd,
		&r.Ordinal, &r.BoundarySource, &r.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return SceneRow{}, fmt.Errorf("scene %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return SceneRow{}, fmt.Errorf("inspect scene %s: %w", id, err)
	}
	return r, nil
}

// InspectSceneCard returns a scene card by scene ID.
func (s *Store) InspectSceneCard(sceneID string) (SceneCardRow, error) {
	var r SceneCardRow
	var evJSON string
	err := s.db.QueryRow(
		`SELECT scene_id, title, summary, evidence_json, generation_run,
		        generation_model, prompt_version, status, raw_json
		 FROM scene_cards WHERE scene_id = ?`, sceneID,
	).Scan(&r.SceneID, &r.Title, &r.Summary, &evJSON,
		&r.GenerationRun, &r.GenerationModel, &r.PromptVersion, &r.Status, &r.RawJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return SceneCardRow{}, fmt.Errorf("scene card %s: %w", sceneID, ErrNotFound)
	}
	if err != nil {
		return SceneCardRow{}, fmt.Errorf("inspect scene card %s: %w", sceneID, err)
	}
	if err := json.Unmarshal([]byte(evJSON), &r.Evidence); err != nil {
		return SceneCardRow{}, fmt.Errorf("parse evidence for scene card %s: %w", sceneID, err)
	}
	return r, nil
}

// SceneCounts returns the number of scenes and scene cards in the index.
func (s *Store) SceneCounts() (scenes, cards int, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM scenes`).Scan(&scenes); err != nil {
		return 0, 0, fmt.Errorf("count scenes: %w", err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM scene_cards`).Scan(&cards); err != nil {
		return 0, 0, fmt.Errorf("count scene cards: %w", err)
	}
	return scenes, cards, nil
}

// DeleteScenesForChapter removes all scenes (and their cards) for a chapter.
func (s *Store) DeleteScenesForChapter(chapterID string) error {
	// Collect scene IDs first so we can clean FTS.
	rows, err := s.db.Query(`SELECT id FROM scenes WHERE chapter_id = ?`, chapterID)
	if err != nil {
		return fmt.Errorf("list scenes for chapter %s: %w", chapterID, err)
	}
	var sceneIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		sceneIDs = append(sceneIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Remove FTS entries for the affected scene cards.
	for _, id := range sceneIDs {
		s.db.Exec(`DELETE FROM scene_cards_fts WHERE scene_id = ?`, id) //nolint:errcheck
	}

	// scene_cards references scenes by scene_id, so delete cards first.
	_, err = s.db.Exec(
		`DELETE FROM scene_cards WHERE scene_id IN
		 (SELECT id FROM scenes WHERE chapter_id = ?)`, chapterID,
	)
	if err != nil {
		return fmt.Errorf("delete scene cards for chapter %s: %w", chapterID, err)
	}
	_, err = s.db.Exec(`DELETE FROM scenes WHERE chapter_id = ?`, chapterID)
	if err != nil {
		return fmt.Errorf("delete scenes for chapter %s: %w", chapterID, err)
	}
	_, err = s.db.Exec(`DELETE FROM chapter_scene_snapshots WHERE chapter_id = ?`, chapterID)
	if err != nil {
		return fmt.Errorf("delete chapter snapshot %s: %w", chapterID, err)
	}
	return nil
}

// SceneBreakOrdinals returns the block ordinals (1-based) of all scene_break
// blocks for the given chapter.  This is used by the compiler to detect
// explicit scene boundaries without importing the full manuscript package.
func (s *Store) SceneBreakOrdinals(chapterID string) ([]int, error) {
	rows, err := s.db.Query(
		`SELECT ordinal FROM blocks WHERE chapter_id = ? AND block_type = 'scene_break' ORDER BY ordinal`,
		chapterID,
	)
	if err != nil {
		return nil, fmt.Errorf("scene break ordinals for chapter %s: %w", chapterID, err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var ord int
		if err := rows.Scan(&ord); err != nil {
			return nil, err
		}
		out = append(out, ord)
	}
	return out, rows.Err()
}

// MarkChapterSnapshotCommitted records that all scenes for chapterID have been
// fully written.  committedAt is the RFC3339 timestamp from the
// chapter_snapshot record in model/scenes.jsonl.
func (s *Store) MarkChapterSnapshotCommitted(chapterID, committedAt string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO chapter_scene_snapshots (chapter_id, committed_at) VALUES (?, ?)`,
		chapterID, committedAt,
	)
	if err != nil {
		return fmt.Errorf("mark chapter snapshot %s: %w", chapterID, err)
	}
	return nil
}

// IsChapterSnapshotCommitted reports whether an explicit snapshot has been
// committed for chapterID, meaning all scenes were written and validated.
func (s *Store) IsChapterSnapshotCommitted(chapterID string) (bool, error) {
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM chapter_scene_snapshots WHERE chapter_id = ?`, chapterID,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("check chapter snapshot %s: %w", chapterID, err)
	}
	return n > 0, nil
}

// DeleteChapterSnapshot removes the snapshot commitment record for a chapter.
// It does not remove the scenes themselves; call DeleteScenesForChapter for
// that.
func (s *Store) DeleteChapterSnapshot(chapterID string) error {
	_, err := s.db.Exec(
		`DELETE FROM chapter_scene_snapshots WHERE chapter_id = ?`, chapterID,
	)
	if err != nil {
		return fmt.Errorf("delete chapter snapshot %s: %w", chapterID, err)
	}
	return nil
}

// AllSceneCards returns all scene card rows ordered by their scene's chapter
// ordinal and scene ordinal.
func (s *Store) AllSceneCards() ([]SceneCardRow, error) {
	rows, err := s.db.Query(
		`SELECT sc.scene_id, sc.title, sc.summary, sc.evidence_json,
		        sc.generation_run, sc.generation_model, sc.prompt_version,
		        sc.status, sc.raw_json
		 FROM scene_cards sc
		 JOIN scenes sn ON sn.id = sc.scene_id
		 JOIN chapters c ON c.id = sn.chapter_id
		 ORDER BY c.ordinal, sn.ordinal`,
	)
	if err != nil {
		return nil, fmt.Errorf("all scene cards: %w", err)
	}
	defer rows.Close()
	return scanSceneCardRows(rows)
}

// ParagraphsInRange returns all paragraphs whose ordinal falls between startOrd
// and endOrd (inclusive), for a specific chapter.
func (s *Store) ParagraphsInRange(chapterID string, startOrd, endOrd int) ([]ParagraphRow, error) {
	rows, err := s.db.Query(
		`SELECT id, chapter_id, ordinal, block_type, text, text_hash,
		        source_file, source_line_start, source_line_end
		 FROM paragraphs
		 WHERE chapter_id = ? AND ordinal >= ? AND ordinal <= ?
		 ORDER BY ordinal`,
		chapterID, startOrd, endOrd,
	)
	if err != nil {
		return nil, fmt.Errorf("paragraphs in range for chapter %s: %w", chapterID, err)
	}
	defer rows.Close()
	var out []ParagraphRow
	for rows.Next() {
		var p ParagraphRow
		if err := rows.Scan(&p.ID, &p.ChapterID, &p.Ordinal, &p.BlockType,
			&p.Text, &p.TextHash, &p.SourceFile, &p.SourceLineStart, &p.SourceLineEnd); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// scanSceneCardRows scans rows from the scene_cards table.
func scanSceneCardRows(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]SceneCardRow, error) {
	var out []SceneCardRow
	for rows.Next() {
		var r SceneCardRow
		var evJSON string
		if err := rows.Scan(&r.SceneID, &r.Title, &r.Summary, &evJSON,
			&r.GenerationRun, &r.GenerationModel, &r.PromptVersion, &r.Status, &r.RawJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(evJSON), &r.Evidence); err != nil {
			return nil, fmt.Errorf("parse evidence for scene card %s: %w", r.SceneID, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
