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

// EntityRow is an entity record read from the index.
type EntityRow struct {
	ID              string
	Type            string
	CanonicalName   string
	Aliases         []string
	Evidence        []string
	GenerationRun   string
	GenerationModel string
	PromptVersion   string
	Status          string
	RawJSON         string
}

// MentionRow is an entity mention record read from the index.
type MentionRow struct {
	EntityID        string
	ChapterID       string
	ParagraphID     string
	SurfaceText     string
	Confidence      float64
	GenerationRun   string
	GenerationModel string
	PromptVersion   string
	Status          string
	RawJSON         string
}

type entityJSONLRecord struct {
	RecordType    string   `json:"record_type"`
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	CanonicalName string   `json:"canonical_name"`
	Aliases       []string `json:"aliases"`
	Evidence      []string `json:"evidence"`
	Generation    struct {
		RunID         string `json:"run_id"`
		Model         string `json:"model"`
		PromptVersion string `json:"prompt_version"`
	} `json:"generation"`
	Status string `json:"status"`
}

type mentionJSONLRecord struct {
	RecordType  string  `json:"record_type"`
	EntityID    string  `json:"entity_id"`
	ChapterID   string  `json:"chapter_id"`
	ParagraphID string  `json:"paragraph_id"`
	SurfaceText string  `json:"surface_text"`
	Confidence  float64 `json:"confidence"`
	Generation  struct {
		RunID         string `json:"run_id"`
		Model         string `json:"model"`
		PromptVersion string `json:"prompt_version"`
	} `json:"generation"`
	Status string `json:"status"`
}

// entitySnapshotJSONLRecord is the explicit commit marker appended to
// model/entities.jsonl after all entity records for a chapter and their
// corresponding mention records in model/mentions.jsonl have been written.
// Only chapters with this marker are treated as fully snapshotted.
type entitySnapshotJSONLRecord struct {
	RecordType   string `json:"record_type"` // "entity_snapshot"
	ChapterID    string `json:"chapter_id"`
	EntityCount  *int   `json:"entity_count"`
	MentionCount *int   `json:"mention_count"`
	CommittedAt  string `json:"committed_at"`
}

type entityCandidate struct {
	record entityJSONLRecord
	line   int
	raw    string
}

type mentionCandidate struct {
	record mentionJSONLRecord
	line   int
	raw    string
}

type entitySnapshotCandidate struct {
	record entitySnapshotJSONLRecord
	line   int
}

type entityPendingBatch struct {
	runID    string
	entities map[string]entityCandidate
}

// InsertEntity inserts or replaces one entity row.
func (s *Store) InsertEntity(r EntityRow) error {
	aliasesJSON, err := json.Marshal(r.Aliases)
	if err != nil {
		return fmt.Errorf("marshal aliases for entity %s: %w", r.ID, err)
	}
	evidenceJSON, err := json.Marshal(r.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence for entity %s: %w", r.ID, err)
	}
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO entities
			(id, type, canonical_name, aliases_json, evidence_json, generation_run,
			 generation_model, prompt_version, status, raw_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Type, r.CanonicalName, string(aliasesJSON), string(evidenceJSON),
		r.GenerationRun, r.GenerationModel, r.PromptVersion, r.Status, r.RawJSON,
	)
	if err != nil {
		return fmt.Errorf("insert entity %s: %w", r.ID, err)
	}
	return nil
}

// InsertMention inserts or replaces one mention row.
func (s *Store) InsertMention(r MentionRow) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO mentions
			(entity_id, chapter_id, paragraph_id, surface_text, confidence, generation_run,
			 generation_model, prompt_version, status, raw_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.EntityID, r.ChapterID, r.ParagraphID, r.SurfaceText, r.Confidence,
		r.GenerationRun, r.GenerationModel, r.PromptVersion, r.Status, r.RawJSON,
	)
	if err != nil {
		return fmt.Errorf("insert mention %s/%s: %w", r.EntityID, r.ParagraphID, err)
	}
	return nil
}

// DeleteEntityMentionsForChapter removes indexed entity mentions for a chapter,
// clears the chapter entity snapshot, and then drops entities that no longer
// have any mentions.
func (s *Store) DeleteEntityMentionsForChapter(chapterID string) (retErr error) {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete entity mentions for chapter %s: %w", chapterID, err)
	}
	defer func() {
		if retErr != nil {
			tx.Rollback()
		}
	}()
	if _, err := tx.Exec(`DELETE FROM mentions WHERE chapter_id = ?`, chapterID); err != nil {
		return fmt.Errorf("delete entity mentions for chapter %s: %w", chapterID, err)
	}
	if _, err := tx.Exec(`DELETE FROM chapter_entity_snapshots WHERE chapter_id = ?`, chapterID); err != nil {
		return fmt.Errorf("delete chapter entity snapshot %s: %w", chapterID, err)
	}
	if _, err := tx.Exec(`DELETE FROM entities WHERE id NOT IN (SELECT DISTINCT entity_id FROM mentions)`); err != nil {
		return fmt.Errorf("delete orphan entities after chapter %s: %w", chapterID, err)
	}
	return tx.Commit()
}

// MarkEntitySnapshotCommitted records that entity extraction for chapterID was
// completely committed to canonical JSONL.
func (s *Store) MarkEntitySnapshotCommitted(chapterID string, entityCount, mentionCount int, committedAt string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO chapter_entity_snapshots (chapter_id, entity_count, mention_count, committed_at)
		 VALUES (?, ?, ?, ?)`,
		chapterID, entityCount, mentionCount, committedAt,
	)
	if err != nil {
		return fmt.Errorf("mark entity snapshot %s: %w", chapterID, err)
	}
	return nil
}

// IsEntitySnapshotCommitted reports whether entity extraction has been fully
// committed for chapterID.
func (s *Store) IsEntitySnapshotCommitted(chapterID string) (bool, error) {
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM chapter_entity_snapshots WHERE chapter_id = ?`, chapterID,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("check entity snapshot %s: %w", chapterID, err)
	}
	return n > 0, nil
}

// EntityMentionCountByChapter returns the number of indexed mentions in a chapter.
func (s *Store) EntityMentionCountByChapter(chapterID string) (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM mentions WHERE chapter_id = ?`, chapterID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count entity mentions for chapter %s: %w", chapterID, err)
	}
	return n, nil
}

// EntityCounts returns indexed entity and mention counts.
func (s *Store) EntityCounts() (entities, mentions int, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM entities`).Scan(&entities); err != nil {
		return 0, 0, fmt.Errorf("count entities: %w", err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM mentions`).Scan(&mentions); err != nil {
		return 0, 0, fmt.Errorf("count mentions: %w", err)
	}
	return entities, mentions, nil
}

// IndexEntitiesJSONL replays committed model/entities.jsonl snapshots and
// their model/mentions.jsonl records into the index.
func (s *Store) IndexEntitiesJSONL(entitiesPath, mentionsPath string) (retErr error) {
	chapterIDs, paragraphs, err := s.entityReplayRefs()
	if err != nil {
		return err
	}

	entities, entityChapterByID, allEntityIDs, snapshots, err := readCommittedEntityJSONL(entitiesPath, chapterIDs, paragraphs)
	if err != nil {
		return err
	}
	mentions, mentionCountsByChapter, err := readCommittedMentionJSONL(mentionsPath, paragraphs, entityChapterByID, allEntityIDs)
	if err != nil {
		return err
	}
	for chapterID, snap := range snapshots {
		want := 0
		if snap.record.MentionCount != nil {
			want = *snap.record.MentionCount
		}
		if got := mentionCountsByChapter[chapterID]; got != want {
			return fmt.Errorf(
				"index entities jsonl: %s:%d: entity_snapshot mention_count mismatch for %s: declared %d, committed %d",
				entitiesPath, snap.line, chapterID, want, got,
			)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("index entities jsonl: %w", err)
	}
	defer func() {
		if retErr != nil {
			tx.Rollback()
		}
	}()
	for _, stmt := range []string{`DELETE FROM mentions`, `DELETE FROM entities`, `DELETE FROM chapter_entity_snapshots`} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("index entities jsonl: %w", err)
		}
	}

	entityIDs := sortedEntityIDs(entities)
	for _, id := range entityIDs {
		cand := entities[id]
		rec := cand.record
		aliasesJSON, _ := json.Marshal(rec.Aliases)
		evidenceJSON, _ := json.Marshal(rec.Evidence)
		if _, err := tx.Exec(
			`INSERT INTO entities
				(id, type, canonical_name, aliases_json, evidence_json, generation_run,
				 generation_model, prompt_version, status, raw_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rec.ID, rec.Type, rec.CanonicalName, string(aliasesJSON), string(evidenceJSON),
			rec.Generation.RunID, rec.Generation.Model, rec.Generation.PromptVersion,
			rec.Status, cand.raw,
		); err != nil {
			return fmt.Errorf("index entities jsonl: insert entity %s: %w", rec.ID, err)
		}
	}

	mentionKeys := sortedMentionKeys(mentions)
	for _, key := range mentionKeys {
		cand := mentions[key]
		rec := cand.record
		if _, err := tx.Exec(
			`INSERT INTO mentions
				(entity_id, chapter_id, paragraph_id, surface_text, confidence, generation_run,
				 generation_model, prompt_version, status, raw_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rec.EntityID, rec.ChapterID, rec.ParagraphID, rec.SurfaceText, rec.Confidence,
			rec.Generation.RunID, rec.Generation.Model, rec.Generation.PromptVersion,
			rec.Status, cand.raw,
		); err != nil {
			return fmt.Errorf("index mentions jsonl: insert mention %s/%s: %w", rec.EntityID, rec.ParagraphID, err)
		}
	}

	chapterSnapshotIDs := sortedEntitySnapshotChapters(snapshots)
	for _, chapterID := range chapterSnapshotIDs {
		snap := snapshots[chapterID].record
		entityCount := 0
		if snap.EntityCount != nil {
			entityCount = *snap.EntityCount
		}
		mentionCount := 0
		if snap.MentionCount != nil {
			mentionCount = *snap.MentionCount
		}
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO chapter_entity_snapshots (chapter_id, entity_count, mention_count, committed_at)
			 VALUES (?, ?, ?, ?)`,
			chapterID, entityCount, mentionCount, snap.CommittedAt,
		); err != nil {
			return fmt.Errorf("index entities jsonl: update entity snapshot %s: %w", chapterID, err)
		}
	}
	return tx.Commit()
}

func (s *Store) entityReplayRefs() (map[string]bool, map[string]string, error) {
	chapterIDs := make(map[string]bool)
	chapterRows, err := s.db.Query(`SELECT id FROM chapters`)
	if err != nil {
		return nil, nil, fmt.Errorf("index entities jsonl: %w", err)
	}
	for chapterRows.Next() {
		var id string
		if err := chapterRows.Scan(&id); err != nil {
			chapterRows.Close()
			return nil, nil, fmt.Errorf("index entities jsonl: %w", err)
		}
		chapterIDs[id] = true
	}
	chapterRows.Close()
	if err := chapterRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("index entities jsonl: %w", err)
	}

	paragraphs := make(map[string]string)
	rows, err := s.db.Query(`SELECT id, chapter_id FROM paragraphs`)
	if err != nil {
		return nil, nil, fmt.Errorf("index entities jsonl: %w", err)
	}
	for rows.Next() {
		var id, chapterID string
		if err := rows.Scan(&id, &chapterID); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("index entities jsonl: %w", err)
		}
		paragraphs[id] = chapterID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("index entities jsonl: %w", err)
	}
	return chapterIDs, paragraphs, nil
}

func readCommittedEntityJSONL(
	path string,
	chapterIDs map[string]bool,
	paragraphs map[string]string,
) (map[string]entityCandidate, map[string]string, map[string]bool, map[string]entitySnapshotCandidate, error) {
	pendingByChapter := make(map[string]entityPendingBatch)
	committedByChapter := make(map[string]map[string]entityCandidate)
	snapshots := make(map[string]entitySnapshotCandidate)
	allEntityIDs := make(map[string]bool)

	if err := scanJSONL(path, func(lineNo int, line []byte) error {
		var typed struct {
			RecordType string `json:"record_type"`
		}
		if err := json.Unmarshal(line, &typed); err != nil {
			return fmt.Errorf("index entities jsonl: %s:%d: malformed json: %w", path, lineNo, err)
		}
		switch typed.RecordType {
		case "entity":
			var rec entityJSONLRecord
			if err := json.Unmarshal(line, &rec); err != nil {
				return fmt.Errorf("index entities jsonl: %s:%d: malformed entity record: %w", path, lineNo, err)
			}
			if strings.TrimSpace(rec.ID) == "" {
				return fmt.Errorf("index entities jsonl: %s:%d: entity missing id", path, lineNo)
			}
			allEntityIDs[rec.ID] = true
			chapterID, err := entityRecordChapter(path, lineNo, rec, paragraphs)
			if err != nil {
				return err
			}
			runID := strings.TrimSpace(rec.Generation.RunID)
			batch := pendingByChapter[chapterID]
			if batch.entities == nil || batch.runID != runID {
				batch = entityPendingBatch{runID: runID, entities: make(map[string]entityCandidate)}
			}
			batch.entities[rec.ID] = entityCandidate{record: rec, line: lineNo, raw: string(line)}
			pendingByChapter[chapterID] = batch
		case "entity_snapshot":
			var snap entitySnapshotJSONLRecord
			if err := json.Unmarshal(line, &snap); err != nil {
				return fmt.Errorf("index entities jsonl: %s:%d: malformed entity_snapshot record: %w", path, lineNo, err)
			}
			if strings.TrimSpace(snap.ChapterID) == "" {
				return fmt.Errorf("index entities jsonl: %s:%d: entity_snapshot missing chapter_id", path, lineNo)
			}
			if !chapterIDs[snap.ChapterID] {
				return fmt.Errorf("index entities jsonl: %s:%d: entity_snapshot references missing chapter %q", path, lineNo, snap.ChapterID)
			}
			if snap.EntityCount == nil {
				return fmt.Errorf("index entities jsonl: %s:%d: entity_snapshot missing entity_count", path, lineNo)
			}
			if snap.MentionCount == nil {
				return fmt.Errorf("index entities jsonl: %s:%d: entity_snapshot missing mention_count", path, lineNo)
			}
			if *snap.EntityCount < 0 {
				return fmt.Errorf("index entities jsonl: %s:%d: entity_snapshot has invalid entity_count %d", path, lineNo, *snap.EntityCount)
			}
			if *snap.MentionCount < 0 {
				return fmt.Errorf("index entities jsonl: %s:%d: entity_snapshot has invalid mention_count %d", path, lineNo, *snap.MentionCount)
			}
			pending := pendingByChapter[snap.ChapterID]
			pendingCount := 0
			if pending.entities != nil {
				pendingCount = len(pending.entities)
			}
			if pendingCount != *snap.EntityCount {
				return fmt.Errorf(
					"index entities jsonl: %s:%d: entity_snapshot entity_count mismatch for %s: declared %d, pending %d",
					path, lineNo, snap.ChapterID, *snap.EntityCount, pendingCount,
				)
			}
			committed := make(map[string]entityCandidate, pendingCount)
			for id, cand := range pending.entities {
				committed[id] = cand
			}
			committedByChapter[snap.ChapterID] = committed
			snapshots[snap.ChapterID] = entitySnapshotCandidate{record: snap, line: lineNo}
			delete(pendingByChapter, snap.ChapterID)
		default:
			// Ignore unsupported record types for forward compatibility.
		}
		return nil
	}); err != nil {
		return nil, nil, nil, nil, err
	}

	committedEntities := make(map[string]entityCandidate)
	entityChapterByID := make(map[string]string)
	for chapterID, entities := range committedByChapter {
		for id, cand := range entities {
			if _, exists := committedEntities[id]; exists {
				return nil, nil, nil, nil, fmt.Errorf("index entities jsonl: duplicate committed entity id %q", id)
			}
			committedEntities[id] = cand
			entityChapterByID[id] = chapterID
		}
	}
	return committedEntities, entityChapterByID, allEntityIDs, snapshots, nil
}

func entityRecordChapter(path string, lineNo int, rec entityJSONLRecord, paragraphs map[string]string) (string, error) {
	if len(rec.Evidence) == 0 {
		return "", fmt.Errorf("index entities jsonl: %s:%d: entity %s has no evidence", path, lineNo, rec.ID)
	}
	chapterID := ""
	for _, pid := range rec.Evidence {
		pid = strings.TrimSpace(pid)
		if pid == "" {
			return "", fmt.Errorf("index entities jsonl: %s:%d: entity %s has blank evidence paragraph", path, lineNo, rec.ID)
		}
		ch, ok := paragraphs[pid]
		if !ok {
			return "", fmt.Errorf("index entities jsonl: %s:%d: entity %s references missing evidence paragraph %q", path, lineNo, rec.ID, pid)
		}
		if chapterID == "" {
			chapterID = ch
		} else if chapterID != ch {
			return "", fmt.Errorf("index entities jsonl: %s:%d: entity %s evidence spans chapters %s and %s", path, lineNo, rec.ID, chapterID, ch)
		}
	}
	return chapterID, nil
}

func readCommittedMentionJSONL(
	path string,
	paragraphs map[string]string,
	entityChapterByID map[string]string,
	allEntityIDs map[string]bool,
) (map[string]mentionCandidate, map[string]int, error) {
	out := make(map[string]mentionCandidate)
	if err := scanJSONL(path, func(lineNo int, line []byte) error {
		var typed struct {
			RecordType string `json:"record_type"`
		}
		if err := json.Unmarshal(line, &typed); err != nil {
			return fmt.Errorf("index mentions jsonl: %s:%d: malformed json: %w", path, lineNo, err)
		}
		if typed.RecordType != "mention" {
			return nil
		}
		var rec mentionJSONLRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return fmt.Errorf("index mentions jsonl: %s:%d: malformed mention record: %w", path, lineNo, err)
		}
		entityID := strings.TrimSpace(rec.EntityID)
		entityChapter, committed := entityChapterByID[entityID]
		if !committed {
			if allEntityIDs[entityID] {
				return nil
			}
			return fmt.Errorf("index mentions jsonl: %s:%d: mention references missing entity %q", path, lineNo, rec.EntityID)
		}
		chapterID, ok := paragraphs[rec.ParagraphID]
		if !ok {
			return fmt.Errorf("index mentions jsonl: %s:%d: mention references missing paragraph %q", path, lineNo, rec.ParagraphID)
		}
		if rec.ChapterID == "" {
			rec.ChapterID = chapterID
		} else if rec.ChapterID != chapterID {
			return fmt.Errorf("index mentions jsonl: %s:%d: mention chapter %s does not match paragraph chapter %s", path, lineNo, rec.ChapterID, chapterID)
		}
		if entityChapter != chapterID {
			return fmt.Errorf("index mentions jsonl: %s:%d: mention chapter %s does not match entity chapter %s", path, lineNo, chapterID, entityChapter)
		}
		key := rec.EntityID + "\x00" + rec.ParagraphID + "\x00" + rec.SurfaceText
		out[key] = mentionCandidate{record: rec, line: lineNo, raw: string(line)}
		return nil
	}); err != nil {
		return nil, nil, err
	}

	counts := make(map[string]int)
	for _, cand := range out {
		counts[cand.record.ChapterID]++
	}
	return out, counts, nil
}

func sortedEntityIDs(values map[string]entityCandidate) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedMentionKeys(values map[string]mentionCandidate) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedEntitySnapshotChapters(values map[string]entitySnapshotCandidate) []string {
	chapters := make([]string, 0, len(values))
	for chapterID := range values {
		chapters = append(chapters, chapterID)
	}
	sort.Strings(chapters)
	return chapters
}

func scanJSONL(path string, fn func(lineNo int, line []byte) error) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := fn(lineNo, line); err != nil {
			return err
		}
	}
	return sc.Err()
}
