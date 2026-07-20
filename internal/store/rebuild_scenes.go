package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

type sceneJSONLRecord struct {
	RecordType     string `json:"record_type"`
	ID             string `json:"id"`
	ChapterID      string `json:"chapter_id"`
	ParagraphStart string `json:"paragraph_start"`
	ParagraphEnd   string `json:"paragraph_end"`
	Ordinal        int    `json:"ordinal"`
	BoundarySource string `json:"boundary_source"`
	Status         string `json:"status"`
}

type sceneCardJSONLRecord struct {
	RecordType string   `json:"record_type"`
	SceneID    string   `json:"scene_id"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Evidence   []string `json:"evidence"`
	Generation struct {
		RunID         string `json:"run_id"`
		Model         string `json:"model"`
		PromptVersion string `json:"prompt_version"`
	} `json:"generation"`
	Status string `json:"status"`
}

type paragraphRef struct {
	ChapterID string
	Ordinal   int
}

type sceneCandidate struct {
	record sceneJSONLRecord
	line   int
}

type sceneCardCandidate struct {
	record sceneCardJSONLRecord
	line   int
	raw    string
}

// IndexScenesJSONL replays model/scenes.jsonl into scenes and scene_cards.
//
// Canonical conflict handling is deterministic:
//   - scene records are keyed by (chapter_id, ordinal)
//   - a scene record with ordinal==1 starts a replacement snapshot for that chapter
//   - within a snapshot, the latest valid line for a key wins
//   - scene cards are keyed by scene_id and latest valid line wins
//
// Scene cards that reference superseded historical scene IDs are ignored.
func (s *Store) IndexScenesJSONL(path string) (retErr error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("index scenes jsonl: %w", err)
	}
	defer f.Close()

	chapters, err := s.AllChapters()
	if err != nil {
		return err
	}
	chapterIDs := make(map[string]bool, len(chapters))
	for _, ch := range chapters {
		chapterIDs[ch.ID] = true
	}

	paragraphs := make(map[string]paragraphRef)
	rows, err := s.db.Query(`SELECT id, chapter_id, ordinal FROM paragraphs`)
	if err != nil {
		return fmt.Errorf("index scenes jsonl: %w", err)
	}
	for rows.Next() {
		var id, chapterID string
		var ordinal int
		if err := rows.Scan(&id, &chapterID, &ordinal); err != nil {
			rows.Close()
			return fmt.Errorf("index scenes jsonl: %w", err)
		}
		paragraphs[id] = paragraphRef{ChapterID: chapterID, Ordinal: ordinal}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("index scenes jsonl: %w", err)
	}

	sceneByChapterOrdinal := make(map[string]map[int]sceneCandidate)
	historicalSceneIDs := make(map[string]bool)
	latestCards := make(map[string]sceneCardCandidate)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var typed struct {
			RecordType string `json:"record_type"`
		}
		if err := json.Unmarshal(line, &typed); err != nil {
			return fmt.Errorf("index scenes jsonl: %s:%d: malformed json: %w", path, lineNo, err)
		}
		switch typed.RecordType {
		case "scene":
			var rec sceneJSONLRecord
			if err := json.Unmarshal(line, &rec); err != nil {
				return fmt.Errorf("index scenes jsonl: %s:%d: malformed scene record: %w", path, lineNo, err)
			}
			if strings.TrimSpace(rec.ID) == "" {
				return fmt.Errorf("index scenes jsonl: %s:%d: scene record missing id", path, lineNo)
			}
			if strings.TrimSpace(rec.ChapterID) == "" {
				return fmt.Errorf("index scenes jsonl: %s:%d: scene %s missing chapter_id", path, lineNo, rec.ID)
			}
			if rec.Ordinal <= 0 {
				return fmt.Errorf("index scenes jsonl: %s:%d: scene %s has invalid ordinal %d", path, lineNo, rec.ID, rec.Ordinal)
			}
			historicalSceneIDs[rec.ID] = true
			chMap := sceneByChapterOrdinal[rec.ChapterID]
			if rec.Ordinal == 1 || chMap == nil {
				chMap = make(map[int]sceneCandidate)
				sceneByChapterOrdinal[rec.ChapterID] = chMap
			}
			chMap[rec.Ordinal] = sceneCandidate{record: rec, line: lineNo}
		case "scene_card":
			var rec sceneCardJSONLRecord
			if err := json.Unmarshal(line, &rec); err != nil {
				return fmt.Errorf("index scenes jsonl: %s:%d: malformed scene_card record: %w", path, lineNo, err)
			}
			if strings.TrimSpace(rec.SceneID) == "" {
				return fmt.Errorf("index scenes jsonl: %s:%d: scene_card record missing scene_id", path, lineNo)
			}
			latestCards[rec.SceneID] = sceneCardCandidate{record: rec, line: lineNo, raw: string(line)}
		default:
			// Ignore unsupported record types for forward compatibility.
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("index scenes jsonl: %s: %w", path, err)
	}

	unknownChapters := make([]string, 0)
	for chapterID := range sceneByChapterOrdinal {
		if !chapterIDs[chapterID] {
			unknownChapters = append(unknownChapters, chapterID)
		}
	}
	sort.Strings(unknownChapters)
	if len(unknownChapters) > 0 {
		m := sceneByChapterOrdinal[unknownChapters[0]]
		var minLine int
		for _, cand := range m {
			if minLine == 0 || cand.line < minLine {
				minLine = cand.line
			}
		}
		return fmt.Errorf("index scenes jsonl: %s:%d: scene references missing chapter %q", path, minLine, unknownChapters[0])
	}

	var finalScenes []sceneJSONLRecord
	finalSceneByID := make(map[string]sceneJSONLRecord)
	for _, ch := range chapters {
		chMap := sceneByChapterOrdinal[ch.ID]
		if len(chMap) == 0 {
			continue
		}
		ordinals := make([]int, 0, len(chMap))
		for ord := range chMap {
			ordinals = append(ordinals, ord)
		}
		sort.Ints(ordinals)
		for _, ord := range ordinals {
			cand := chMap[ord]
			rec := cand.record
			start, ok := paragraphs[rec.ParagraphStart]
			if !ok {
				return fmt.Errorf("index scenes jsonl: %s:%d: scene %s references missing paragraph_start %q", path, cand.line, rec.ID, rec.ParagraphStart)
			}
			end, ok := paragraphs[rec.ParagraphEnd]
			if !ok {
				return fmt.Errorf("index scenes jsonl: %s:%d: scene %s references missing paragraph_end %q", path, cand.line, rec.ID, rec.ParagraphEnd)
			}
			if start.ChapterID != rec.ChapterID || end.ChapterID != rec.ChapterID {
				return fmt.Errorf("index scenes jsonl: %s:%d: scene %s paragraph range not in chapter %s", path, cand.line, rec.ID, rec.ChapterID)
			}
			if start.Ordinal > end.Ordinal {
				return fmt.Errorf("index scenes jsonl: %s:%d: scene %s has paragraph_start after paragraph_end", path, cand.line, rec.ID)
			}
			if _, exists := finalSceneByID[rec.ID]; exists {
				return fmt.Errorf("index scenes jsonl: %s:%d: duplicate active scene id %q", path, cand.line, rec.ID)
			}
			finalScenes = append(finalScenes, rec)
			finalSceneByID[rec.ID] = rec
		}
	}

	for sceneID, cand := range latestCards {
		if _, ok := finalSceneByID[sceneID]; !ok && !historicalSceneIDs[sceneID] {
			return fmt.Errorf("index scenes jsonl: %s:%d: scene_card references missing scene %q", path, cand.line, sceneID)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("index scenes jsonl: %w", err)
	}
	defer func() {
		if retErr != nil {
			tx.Rollback()
		}
	}()
	for _, stmt := range []string{
		`DELETE FROM scene_cards_fts`,
		`DELETE FROM scene_cards`,
		`DELETE FROM scenes`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("index scenes jsonl: %w", err)
		}
	}
	for _, scn := range finalScenes {
		if _, err := tx.Exec(
			`INSERT INTO scenes
				(id, chapter_id, paragraph_start, paragraph_end, ordinal, boundary_source, status)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			scn.ID, scn.ChapterID, scn.ParagraphStart, scn.ParagraphEnd,
			scn.Ordinal, scn.BoundarySource, scn.Status,
		); err != nil {
			return fmt.Errorf("index scenes jsonl: insert scene %s: %w", scn.ID, err)
		}
	}
	for _, scn := range finalScenes {
		cand, ok := latestCards[scn.ID]
		if !ok {
			continue
		}
		rec := cand.record
		for _, pid := range rec.Evidence {
			p, ok := paragraphs[pid]
			if !ok {
				return fmt.Errorf("index scenes jsonl: %s:%d: scene_card %s references missing evidence paragraph %q", path, cand.line, rec.SceneID, pid)
			}
			start := paragraphs[scn.ParagraphStart].Ordinal
			end := paragraphs[scn.ParagraphEnd].Ordinal
			if p.ChapterID != scn.ChapterID || p.Ordinal < start || p.Ordinal > end {
				return fmt.Errorf("index scenes jsonl: %s:%d: scene_card %s evidence paragraph %q not in scene range", path, cand.line, rec.SceneID, pid)
			}
		}
		evidenceJSON, err := json.Marshal(rec.Evidence)
		if err != nil {
			return fmt.Errorf("index scenes jsonl: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO scene_cards
				(scene_id, title, summary, evidence_json, generation_run, generation_model,
				 prompt_version, status, raw_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rec.SceneID, rec.Title, rec.Summary, string(evidenceJSON),
			rec.Generation.RunID, rec.Generation.Model, rec.Generation.PromptVersion,
			rec.Status, cand.raw,
		); err != nil {
			return fmt.Errorf("index scenes jsonl: insert scene_card %s: %w", rec.SceneID, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO scene_cards_fts(scene_id, title, summary) VALUES (?, ?, ?)`,
			rec.SceneID, rec.Title, rec.Summary,
		); err != nil {
			return fmt.Errorf("index scenes jsonl: index scene_card FTS %s: %w", rec.SceneID, err)
		}
	}
	return tx.Commit()
}
