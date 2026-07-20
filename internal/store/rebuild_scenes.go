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

// chapterSnapshotJSONLRecord is the explicit commit marker appended to
// model/scenes.jsonl after all scene records for a chapter have been written.
// Only chapters with this marker are treated as fully snapshotted.
type chapterSnapshotJSONLRecord struct {
	RecordType  string `json:"record_type"` // "chapter_snapshot"
	ChapterID   string `json:"chapter_id"`
	SceneCount  *int   `json:"scene_count"`
	CommittedAt string `json:"committed_at"`
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
//   - scene records are keyed by (chapter_id, ordinal) in a per-chapter
//     pending buffer; a scene with ordinal==1 resets the pending buffer for
//     that chapter
//   - a "chapter_snapshot" record explicitly commits the pending buffer for
//     that chapter; only committed chapters are loaded into the index
//   - within a committed snapshot, the latest valid line for each ordinal wins
//   - scene cards are keyed by scene_id and latest valid line wins
//
// Scene cards that reference superseded historical scene IDs are ignored.
//
// Partition validation: the committed scenes for each chapter must form a
// complete, non-overlapping cover of all paragraphs in that chapter.
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

	// pendingByChapter accumulates scene records for a chapter since the last
	// ordinal==1 reset.  An ordinal==1 scene resets the pending buffer so that
	// a re-run of a chapter overwrites the previous incomplete attempt.
	pendingByChapter := make(map[string]map[int]sceneCandidate)
	// committedByChapter holds the scenes that were explicitly committed via a
	// chapter_snapshot record.  Only these are loaded into the index.
	committedByChapter := make(map[string]map[int]sceneCandidate)
	// committedAtByChapter records the RFC3339 timestamp from each chapter_snapshot.
	committedAtByChapter := make(map[string]string)
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
			chMap := pendingByChapter[rec.ChapterID]
			if rec.Ordinal == 1 || chMap == nil {
				// An ordinal==1 record starts a fresh pending snapshot, discarding
				// any previously accumulated (uncommitted) scenes for this chapter.
				chMap = make(map[int]sceneCandidate)
				pendingByChapter[rec.ChapterID] = chMap
			}
			chMap[rec.Ordinal] = sceneCandidate{record: rec, line: lineNo}
		case "chapter_snapshot":
			var snap chapterSnapshotJSONLRecord
			if err := json.Unmarshal(line, &snap); err != nil {
				return fmt.Errorf("index scenes jsonl: %s:%d: malformed chapter_snapshot record: %w", path, lineNo, err)
			}
			if strings.TrimSpace(snap.ChapterID) == "" {
				return fmt.Errorf("index scenes jsonl: %s:%d: chapter_snapshot missing chapter_id", path, lineNo)
			}
			if snap.SceneCount == nil {
				return fmt.Errorf("index scenes jsonl: %s:%d: chapter_snapshot missing scene_count", path, lineNo)
			}
			if *snap.SceneCount < 0 {
				return fmt.Errorf("index scenes jsonl: %s:%d: chapter_snapshot has invalid scene_count %d", path, lineNo, *snap.SceneCount)
			}
			pending := pendingByChapter[snap.ChapterID]
			pendingCount := 0
			if pending != nil {
				pendingCount = len(pending)
			}
			if pendingCount != *snap.SceneCount {
				return fmt.Errorf(
					"index scenes jsonl: %s:%d: chapter_snapshot scene_count mismatch for %s: declared %d, pending %d",
					path, lineNo, snap.ChapterID, *snap.SceneCount, pendingCount,
				)
			}
			committed := make(map[int]sceneCandidate, pendingCount)
			for ord, cand := range pending {
				committed[ord] = cand
			}
			committedByChapter[snap.ChapterID] = committed
			committedAtByChapter[snap.ChapterID] = snap.CommittedAt
			delete(pendingByChapter, snap.ChapterID)
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

	// Report scenes that reference a chapter not in the index.
	unknownChapters := make([]string, 0)
	for chapterID := range committedByChapter {
		if !chapterIDs[chapterID] {
			unknownChapters = append(unknownChapters, chapterID)
		}
	}
	sort.Strings(unknownChapters)
	if len(unknownChapters) > 0 {
		m := committedByChapter[unknownChapters[0]]
		var minLine int
		for _, cand := range m {
			if minLine == 0 || cand.line < minLine {
				minLine = cand.line
			}
		}
		return fmt.Errorf("index scenes jsonl: %s:%d: scene references missing chapter %q", path, minLine, unknownChapters[0])
	}

	// Build the final scene list from committed snapshots and validate partitions.
	var finalScenes []sceneJSONLRecord
	finalSceneByID := make(map[string]sceneJSONLRecord)
	for _, ch := range chapters {
		chMap, committed := committedByChapter[ch.ID]
		if !committed {
			// No chapter_snapshot record in the JSONL: chapter is uncompiled, skip.
			continue
		}
		ordinals := make([]int, 0, len(chMap))
		for ord := range chMap {
			ordinals = append(ordinals, ord)
		}
		sort.Ints(ordinals)
		var chScenes []sceneJSONLRecord
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
			chScenes = append(chScenes, rec)
		}
		// Validate that committed scenes form a complete partition of the chapter.
		if err := validateJSONLScenePartition(ch.ID, chScenes, paragraphs); err != nil {
			return fmt.Errorf("index scenes jsonl: committed scenes for chapter %s do not form a complete partition: %w", ch.ID, err)
		}
		for _, sc := range chScenes {
			finalScenes = append(finalScenes, sc)
			finalSceneByID[sc.ID] = sc
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
		`DELETE FROM chapter_scene_snapshots`,
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
	// Re-populate chapter_scene_snapshots from committed chapter_snapshot records.
	for chapterID, committedAt := range committedAtByChapter {
		if !chapterIDs[chapterID] {
			continue // already reported as error above; skip stale entries
		}
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO chapter_scene_snapshots (chapter_id, committed_at) VALUES (?, ?)`,
			chapterID, committedAt,
		); err != nil {
			return fmt.Errorf("index scenes jsonl: update chapter snapshot %s: %w", chapterID, err)
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

// validateJSONLScenePartition checks that the scenes for a chapter (sorted by
// ordinal) form a complete, non-overlapping cover of all paragraphs in that
// chapter.  paragraphs maps paragraph IDs to their chapter and ordinal.
func validateJSONLScenePartition(chapterID string, scenes []sceneJSONLRecord, paragraphs map[string]paragraphRef) error {
	// Collect paragraphs belonging to this chapter, sorted by ordinal.
	type paraEntry struct {
		id      string
		ordinal int
	}
	var paras []paraEntry
	for id, ref := range paragraphs {
		if ref.ChapterID == chapterID {
			paras = append(paras, paraEntry{id: id, ordinal: ref.Ordinal})
		}
	}
	sort.Slice(paras, func(i, j int) bool { return paras[i].ordinal < paras[j].ordinal })

	if len(paras) == 0 {
		if len(scenes) != 0 {
			return fmt.Errorf("chapter has no paragraphs but %d committed scene(s)", len(scenes))
		}
		return nil
	}
	if len(scenes) == 0 {
		return fmt.Errorf("chapter has %d paragraph(s) but no committed scenes", len(paras))
	}

	ordByID := make(map[string]int, len(paras))
	for _, p := range paras {
		ordByID[p.id] = p.ordinal
	}
	for i, sc := range scenes {
		want := i + 1
		if sc.Ordinal != want {
			return fmt.Errorf("scene %s has ordinal %d, expected %d", sc.ID, sc.Ordinal, want)
		}
	}

	// First scene must start at the first paragraph.
	if scenes[0].ParagraphStart != paras[0].id {
		return fmt.Errorf("first scene starts at paragraph %q but first paragraph is %q",
			scenes[0].ParagraphStart, paras[0].id)
	}
	// Last scene must end at the last paragraph.
	if scenes[len(scenes)-1].ParagraphEnd != paras[len(paras)-1].id {
		return fmt.Errorf("last scene ends at paragraph %q but last paragraph is %q",
			scenes[len(scenes)-1].ParagraphEnd, paras[len(paras)-1].id)
	}
	// Consecutive scenes must chain with no gap or overlap.
	for i := 1; i < len(scenes); i++ {
		prevEndOrd, ok := ordByID[scenes[i-1].ParagraphEnd]
		if !ok {
			return fmt.Errorf("scene %s paragraph_end %q not in chapter",
				scenes[i-1].ID, scenes[i-1].ParagraphEnd)
		}
		curStartOrd, ok := ordByID[scenes[i].ParagraphStart]
		if !ok {
			return fmt.Errorf("scene %s paragraph_start %q not in chapter",
				scenes[i].ID, scenes[i].ParagraphStart)
		}
		if curStartOrd != prevEndOrd+1 {
			return fmt.Errorf("partition gap between scene %s (ends at paragraph ordinal %d) and scene %s (starts at ordinal %d)",
				scenes[i-1].ID, prevEndOrd, scenes[i].ID, curStartOrd)
		}
	}
	return nil
}
