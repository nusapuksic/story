package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nusapuksic/story/internal/manuscript"
	"github.com/nusapuksic/story/internal/project"
)

// ChapterRow is chapter metadata read from the index.
type ChapterRow struct {
	ID        string
	Ordinal   int
	Title     string
	File      string
	SourceKey string
	// ParagraphCount is populated by InspectChapter.
	ParagraphCount int
}

// ParagraphRow is paragraph metadata read from the index.
type ParagraphRow struct {
	ID              string
	ChapterID       string
	Ordinal         int
	BlockType       string
	Text            string
	TextHash        string
	SourceFile      string
	SourceLineStart int
	SourceLineEnd   int
}

// ImportRow is import-run metadata read from the index.
type ImportRow struct {
	RunID      string
	Type       string
	SourcePath string
	ImportedAt string
	Chapters   int
	Paragraphs int
	Status     string
}

// Rebuild deletes the existing index file and reconstructs it entirely from
// the canonical project files (story.toml, manuscript/toc.toml and the
// canonical chapter files).
func Rebuild(p *project.Project) (retErr error) {
	indexPath := p.Path(project.IndexPath)
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return fmt.Errorf("rebuild index: %w", err)
	}
	tmpPath := indexPath + ".tmp"
	_ = os.Remove(tmpPath)

	s, err := Open(tmpPath)
	if err != nil {
		return err
	}
	defer func() {
		if s != nil {
			if cerr := s.Close(); retErr == nil {
				retErr = cerr
			}
		}
		if retErr != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	tocPath := p.Path(project.TOCPath)
	if _, err := os.Stat(tocPath); errors.Is(err, os.ErrNotExist) {
		// No manuscript imported yet: only index project metadata.
		if err := s.IndexProject(p); err != nil {
			return err
		}
	} else {
		toc, err := manuscript.LoadTOC(tocPath)
		if err != nil {
			return err
		}
		markers := p.Config.Manuscript.SceneBreakMarkers
		chapters := make([]*manuscript.Chapter, 0, len(toc.Chapters))
		for _, entry := range toc.Chapters {
			ch, err := manuscript.LoadChapter(p.Path(project.ManuscriptDir), entry, markers)
			if err != nil {
				return err
			}
			chapters = append(chapters, ch)
		}
		if err := s.IndexProject(p); err != nil {
			return err
		}
		if err := s.IndexChapters(chapters); err != nil {
			return err
		}
		if err := s.IndexScenesJSONL(p.Path(filepath.Join(project.ModelDir, "scenes.jsonl"))); err != nil {
			return err
		}
	}

	if err := s.Close(); err != nil {
		return err
	}
	s = nil
	var movedOld bool
	backupPath := indexPath + ".bak"
	_ = os.Remove(backupPath)
	if err := os.Rename(indexPath, backupPath); err == nil {
		movedOld = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rebuild index: %w", err)
	}
	if err := os.Rename(tmpPath, indexPath); err != nil {
		if movedOld {
			_ = os.Rename(backupPath, indexPath)
		}
		return fmt.Errorf("rebuild index: %w", err)
	}
	if movedOld {
		_ = os.Remove(backupPath)
	}
	return nil
}

// IndexProject stores project metadata in the index.
func (s *Store) IndexProject(p *project.Project) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO projects (project_id, title, language) VALUES (?, ?, ?)`,
		p.Config.ProjectID, p.Config.Title, p.Config.Language,
	)
	if err != nil {
		return fmt.Errorf("index project: %w", err)
	}
	return nil
}

// IndexChapters replaces all chapter, block, and paragraph rows with the
// given canonical chapters.
func (s *Store) IndexChapters(chapters []*manuscript.Chapter) (retErr error) {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("index chapters: %w", err)
	}
	defer func() {
		if retErr != nil {
			tx.Rollback()
		}
	}()
	for _, stmt := range []string{
		`DELETE FROM paragraphs_fts`,
		`DELETE FROM paragraphs`,
		`DELETE FROM blocks`,
		`DELETE FROM chapters`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("index chapters: %w", err)
		}
	}
	for _, ch := range chapters {
		if _, err := tx.Exec(
			`INSERT INTO chapters (id, ordinal, title, file, source_key) VALUES (?, ?, ?, ?, ?)`,
			ch.ID, ch.Order, ch.Title, ch.File, ch.SourceKey,
		); err != nil {
			return fmt.Errorf("index chapter %s: %w", ch.ID, err)
		}
		paragraphOrdinal := 0
		for i, blk := range ch.Blocks {
			var pid any
			if blk.ParagraphID != "" {
				pid = blk.ParagraphID
			}
			if _, err := tx.Exec(
				`INSERT INTO blocks (chapter_id, ordinal, block_type, paragraph_id) VALUES (?, ?, ?, ?)`,
				ch.ID, i+1, string(blk.Type), pid,
			); err != nil {
				return fmt.Errorf("index chapter %s: %w", ch.ID, err)
			}
			if blk.ParagraphID == "" {
				continue
			}
			paragraphOrdinal++
			if _, err := tx.Exec(
				`INSERT INTO paragraphs (id, chapter_id, ordinal, block_type, text, text_hash, source_file, source_line_start, source_line_end)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				blk.ParagraphID, ch.ID, paragraphOrdinal, string(blk.Type), blk.Text,
				manuscript.TextHash(blk.Text), filepath.ToSlash(ch.File), blk.LineStart, blk.LineEnd,
			); err != nil {
				return fmt.Errorf("index paragraph %s: %w", blk.ParagraphID, err)
			}
			if _, err := tx.Exec(
				`INSERT INTO paragraphs_fts(id, chapter_id, text) VALUES (?, ?, ?)`,
				blk.ParagraphID, ch.ID, blk.Text,
			); err != nil {
				return fmt.Errorf("index paragraph FTS %s: %w", blk.ParagraphID, err)
			}
		}
	}
	return tx.Commit()
}

// RecordImport stores an import-run row in the index.
func (s *Store) RecordImport(r ImportRow) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO imports (run_id, type, source_path, imported_at, chapters, paragraphs, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.RunID, r.Type, r.SourcePath, r.ImportedAt, r.Chapters, r.Paragraphs, r.Status,
	)
	if err != nil {
		return fmt.Errorf("record import %s: %w", r.RunID, err)
	}
	return nil
}

// InspectChapter returns chapter metadata by ID.
func (s *Store) InspectChapter(id string) (ChapterRow, error) {
	var c ChapterRow
	err := s.db.QueryRow(
		`SELECT id, ordinal, title, file, source_key,
			(SELECT COUNT(*) FROM paragraphs WHERE chapter_id = chapters.id)
		 FROM chapters WHERE id = ?`, id,
	).Scan(&c.ID, &c.Ordinal, &c.Title, &c.File, &c.SourceKey, &c.ParagraphCount)
	if errors.Is(err, sql.ErrNoRows) {
		return ChapterRow{}, fmt.Errorf("chapter %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return ChapterRow{}, fmt.Errorf("inspect chapter %s: %w", id, err)
	}
	return c, nil
}

// InspectParagraph returns paragraph metadata by ID.
func (s *Store) InspectParagraph(id string) (ParagraphRow, error) {
	var p ParagraphRow
	err := s.db.QueryRow(
		`SELECT id, chapter_id, ordinal, block_type, text, text_hash, source_file, source_line_start, source_line_end
		 FROM paragraphs WHERE id = ?`, id,
	).Scan(&p.ID, &p.ChapterID, &p.Ordinal, &p.BlockType, &p.Text, &p.TextHash, &p.SourceFile, &p.SourceLineStart, &p.SourceLineEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return ParagraphRow{}, fmt.Errorf("paragraph %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return ParagraphRow{}, fmt.Errorf("inspect paragraph %s: %w", id, err)
	}
	return p, nil
}

// Counts returns the chapter and paragraph counts in the index.
func (s *Store) Counts() (chapters, paragraphs int, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM chapters`).Scan(&chapters); err != nil {
		return 0, 0, fmt.Errorf("count chapters: %w", err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM paragraphs`).Scan(&paragraphs); err != nil {
		return 0, 0, fmt.Errorf("count paragraphs: %w", err)
	}
	return chapters, paragraphs, nil
}

// LastImport returns the most recent import row, or ErrNotFound.
func (s *Store) LastImport() (ImportRow, error) {
	var r ImportRow
	err := s.db.QueryRow(
		`SELECT run_id, type, source_path, imported_at, chapters, paragraphs, status
		 FROM imports ORDER BY imported_at DESC, run_id DESC LIMIT 1`,
	).Scan(&r.RunID, &r.Type, &r.SourcePath, &r.ImportedAt, &r.Chapters, &r.Paragraphs, &r.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return ImportRow{}, ErrNotFound
	}
	if err != nil {
		return ImportRow{}, fmt.Errorf("last import: %w", err)
	}
	return r, nil
}
