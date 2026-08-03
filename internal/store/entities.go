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
	ChapterID       string
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

// OccurrenceRow is one scene-scoped entity occurrence read from the index.
type OccurrenceRow struct {
	EntityID        string
	ChapterID       string
	SceneID         string
	SurfaceTexts    []string
	SourceFields    []string
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
	ChapterID     string   `json:"chapter_id"`
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

type occurrenceJSONLRecord struct {
	RecordType   string   `json:"record_type"`
	EntityID     string   `json:"entity_id"`
	ChapterID    string   `json:"chapter_id"`
	SceneID      string   `json:"scene_id"`
	SurfaceTexts []string `json:"surface_texts"`
	SourceFields []string `json:"source_fields"`
	Confidence   float64  `json:"confidence"`
	Generation   struct {
		RunID         string `json:"run_id"`
		Model         string `json:"model"`
		PromptVersion string `json:"prompt_version"`
	} `json:"generation"`
	Status string `json:"status"`
}

// entitySnapshotJSONLRecord is the explicit commit marker appended to
// model/entities.jsonl after all entity records for a chapter and their
// corresponding occurrence records in model/occurrences.jsonl have been
// written. Only chapters with this marker are treated as fully snapshotted.
type entitySnapshotJSONLRecord struct {
	RecordType      string `json:"record_type"` // "entity_snapshot"
	ChapterID       string `json:"chapter_id"`
	EntityCount     *int   `json:"entity_count"`
	OccurrenceCount *int   `json:"occurrence_count"`
	MentionCount    *int   `json:"mention_count"`
	CommittedAt     string `json:"committed_at"`
}

type entityCandidate struct {
	record entityJSONLRecord
	line   int
	raw    string
}

type occurrenceCandidate struct {
	record occurrenceJSONLRecord
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
			(id, chapter_id, type, canonical_name, aliases_json, evidence_json, generation_run,
			 generation_model, prompt_version, status, raw_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.ChapterID, r.Type, r.CanonicalName, string(aliasesJSON), string(evidenceJSON),
		r.GenerationRun, r.GenerationModel, r.PromptVersion, r.Status, r.RawJSON,
	)
	if err != nil {
		return fmt.Errorf("insert entity %s: %w", r.ID, err)
	}
	return nil
}

// InsertOccurrence inserts or replaces one scene-scoped occurrence row.
func (s *Store) InsertOccurrence(r OccurrenceRow) error {
	surfaceTextsJSON, err := json.Marshal(r.SurfaceTexts)
	if err != nil {
		return fmt.Errorf("marshal surface texts for occurrence %s/%s: %w", r.EntityID, r.SceneID, err)
	}
	sourceFieldsJSON, err := json.Marshal(r.SourceFields)
	if err != nil {
		return fmt.Errorf("marshal source fields for occurrence %s/%s: %w", r.EntityID, r.SceneID, err)
	}
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO occurrences
			(entity_id, chapter_id, scene_id, surface_texts_json, source_fields_json, confidence, generation_run,
			 generation_model, prompt_version, status, raw_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.EntityID, r.ChapterID, r.SceneID, string(surfaceTextsJSON), string(sourceFieldsJSON), r.Confidence,
		r.GenerationRun, r.GenerationModel, r.PromptVersion, r.Status, r.RawJSON,
	)
	if err != nil {
		return fmt.Errorf("insert occurrence %s/%s: %w", r.EntityID, r.SceneID, err)
	}
	return nil
}

// DeleteEntityOccurrencesForChapter removes indexed entity occurrences for a
// chapter, clears the chapter entity snapshot, and then drops entities that no
// longer have any occurrences.
func (s *Store) DeleteEntityOccurrencesForChapter(chapterID string) (retErr error) {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete entity occurrences for chapter %s: %w", chapterID, err)
	}
	defer func() {
		if retErr != nil {
			tx.Rollback()
		}
	}()
	if _, err := tx.Exec(`DELETE FROM occurrences WHERE chapter_id = ?`, chapterID); err != nil {
		return fmt.Errorf("delete entity occurrences for chapter %s: %w", chapterID, err)
	}
	if _, err := tx.Exec(`DELETE FROM chapter_entity_snapshots WHERE chapter_id = ?`, chapterID); err != nil {
		return fmt.Errorf("delete chapter entity snapshot %s: %w", chapterID, err)
	}
	if _, err := tx.Exec(`DELETE FROM entities WHERE id NOT IN (SELECT DISTINCT entity_id FROM occurrences)`); err != nil {
		return fmt.Errorf("delete orphan entities after chapter %s: %w", chapterID, err)
	}
	return tx.Commit()
}

// MarkEntitySnapshotCommitted records that entity resolution for chapterID was
// completely committed to canonical JSONL.
func (s *Store) MarkEntitySnapshotCommitted(chapterID string, entityCount, occurrenceCount int, committedAt string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO chapter_entity_snapshots (chapter_id, entity_count, occurrence_count, committed_at)
		 VALUES (?, ?, ?, ?)`,
		chapterID, entityCount, occurrenceCount, committedAt,
	)
	if err != nil {
		return fmt.Errorf("mark entity snapshot %s: %w", chapterID, err)
	}
	return nil
}

// IsEntitySnapshotCommitted reports whether entity resolution has been fully
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

// EntityOccurrenceCountByChapter returns the number of indexed occurrences in a chapter.
func (s *Store) EntityOccurrenceCountByChapter(chapterID string) (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM occurrences WHERE chapter_id = ?`, chapterID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count entity occurrences for chapter %s: %w", chapterID, err)
	}
	return n, nil
}

// EntityCounts returns indexed entity and occurrence counts.
func (s *Store) EntityCounts() (entities, occurrences int, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM entities`).Scan(&entities); err != nil {
		return 0, 0, fmt.Errorf("count entities: %w", err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM occurrences`).Scan(&occurrences); err != nil {
		return 0, 0, fmt.Errorf("count occurrences: %w", err)
	}
	return entities, occurrences, nil
}

// EntityRowsForChapter returns indexed entities, optionally restricted to one
// chapter, ordered by type and canonical name for stable prompt context.
func (s *Store) EntityRowsForChapter(chapterID string) ([]EntityRow, error) {
	query := `SELECT id, chapter_id, type, canonical_name, aliases_json, evidence_json,
		        generation_run, generation_model, prompt_version, status, raw_json
		 FROM entities`
	var args []any
	if strings.TrimSpace(chapterID) != "" {
		query += ` WHERE chapter_id = ?`
		args = append(args, strings.TrimSpace(chapterID))
	}
	query += ` ORDER BY lower(type), lower(canonical_name), id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	defer rows.Close()
	return scanEntityRows(rows)
}

// OccurrencesForEntity returns scene-scoped occurrences for an entity in
// manuscript order. If chapterID is non-empty, results are restricted to it.
func (s *Store) OccurrencesForEntity(entityID, chapterID string, limit int) ([]OccurrenceRow, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	query := `SELECT o.entity_id, o.chapter_id, o.scene_id, o.surface_texts_json,
		        o.source_fields_json, o.confidence, o.generation_run, o.generation_model,
		        o.prompt_version, o.status, o.raw_json
		 FROM occurrences o
		 JOIN chapters c ON c.id = o.chapter_id
		 JOIN scenes sn ON sn.id = o.scene_id
		 WHERE o.entity_id = ?`
	args := []any{entityID}
	if strings.TrimSpace(chapterID) != "" {
		query += ` AND o.chapter_id = ?`
		args = append(args, strings.TrimSpace(chapterID))
	}
	query += ` ORDER BY c.ordinal, sn.ordinal LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("occurrences for entity %s: %w", entityID, err)
	}
	defer rows.Close()
	return scanOccurrenceRows(rows)
}

func scanEntityRows(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]EntityRow, error) {
	var out []EntityRow
	for rows.Next() {
		var r EntityRow
		var aliasesJSON, evidenceJSON string
		if err := rows.Scan(&r.ID, &r.ChapterID, &r.Type, &r.CanonicalName, &aliasesJSON, &evidenceJSON,
			&r.GenerationRun, &r.GenerationModel, &r.PromptVersion, &r.Status, &r.RawJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(aliasesJSON), &r.Aliases); err != nil {
			return nil, fmt.Errorf("parse aliases for entity %s: %w", r.ID, err)
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &r.Evidence); err != nil {
			return nil, fmt.Errorf("parse evidence for entity %s: %w", r.ID, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanOccurrenceRows(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]OccurrenceRow, error) {
	var out []OccurrenceRow
	for rows.Next() {
		var r OccurrenceRow
		var surfacesJSON, sourceFieldsJSON string
		if err := rows.Scan(&r.EntityID, &r.ChapterID, &r.SceneID, &surfacesJSON, &sourceFieldsJSON,
			&r.Confidence, &r.GenerationRun, &r.GenerationModel, &r.PromptVersion, &r.Status, &r.RawJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(surfacesJSON), &r.SurfaceTexts); err != nil {
			return nil, fmt.Errorf("parse surface texts for occurrence %s/%s: %w", r.EntityID, r.SceneID, err)
		}
		if err := json.Unmarshal([]byte(sourceFieldsJSON), &r.SourceFields); err != nil {
			return nil, fmt.Errorf("parse source fields for occurrence %s/%s: %w", r.EntityID, r.SceneID, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// IndexEntitiesJSONL replays committed model/entities.jsonl snapshots and
// their model/occurrences.jsonl records into the index.
func (s *Store) IndexEntitiesJSONL(entitiesPath, occurrencesPath string) (retErr error) {
	chapterIDs, sceneChapterByID, err := s.entityReplayRefs()
	if err != nil {
		return err
	}

	entities, entityChapterByID, allEntityIDs, snapshots, err := readCommittedEntityJSONL(entitiesPath, chapterIDs, sceneChapterByID)
	if err != nil {
		return err
	}
	occurrences, occurrenceCountsByChapter, err := readCommittedOccurrenceJSONL(occurrencesPath, sceneChapterByID, entityChapterByID, allEntityIDs)
	if err != nil {
		return err
	}
	for chapterID, snap := range snapshots {
		want := 0
		if snap.record.OccurrenceCount != nil {
			want = *snap.record.OccurrenceCount
		}
		if got := occurrenceCountsByChapter[chapterID]; got != want {
			return fmt.Errorf(
				"index entities jsonl: %s:%d: entity_snapshot occurrence_count mismatch for %s: declared %d, committed %d",
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
	for _, stmt := range []string{`DELETE FROM occurrences`, `DELETE FROM entities`, `DELETE FROM chapter_entity_snapshots`} {
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
				(id, chapter_id, type, canonical_name, aliases_json, evidence_json, generation_run,
				 generation_model, prompt_version, status, raw_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rec.ID, rec.ChapterID, rec.Type, rec.CanonicalName, string(aliasesJSON), string(evidenceJSON),
			rec.Generation.RunID, rec.Generation.Model, rec.Generation.PromptVersion,
			rec.Status, cand.raw,
		); err != nil {
			return fmt.Errorf("index entities jsonl: insert entity %s: %w", rec.ID, err)
		}
	}

	occurrenceKeys := sortedOccurrenceKeys(occurrences)
	for _, key := range occurrenceKeys {
		cand := occurrences[key]
		rec := cand.record
		surfaceTextsJSON, _ := json.Marshal(rec.SurfaceTexts)
		sourceFieldsJSON, _ := json.Marshal(rec.SourceFields)
		if _, err := tx.Exec(
			`INSERT INTO occurrences
				(entity_id, chapter_id, scene_id, surface_texts_json, source_fields_json, confidence, generation_run,
				 generation_model, prompt_version, status, raw_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rec.EntityID, rec.ChapterID, rec.SceneID, string(surfaceTextsJSON), string(sourceFieldsJSON), rec.Confidence,
			rec.Generation.RunID, rec.Generation.Model, rec.Generation.PromptVersion,
			rec.Status, cand.raw,
		); err != nil {
			return fmt.Errorf("index occurrences jsonl: insert occurrence %s/%s: %w", rec.EntityID, rec.SceneID, err)
		}
	}

	chapterSnapshotIDs := sortedEntitySnapshotChapters(snapshots)
	for _, chapterID := range chapterSnapshotIDs {
		snap := snapshots[chapterID].record
		entityCount := 0
		if snap.EntityCount != nil {
			entityCount = *snap.EntityCount
		}
		occurrenceCount := 0
		if snap.OccurrenceCount != nil {
			occurrenceCount = *snap.OccurrenceCount
		}
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO chapter_entity_snapshots (chapter_id, entity_count, occurrence_count, committed_at)
			 VALUES (?, ?, ?, ?)`,
			chapterID, entityCount, occurrenceCount, snap.CommittedAt,
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

	sceneChapterByID := make(map[string]string)
	rows, err := s.db.Query(`SELECT id, chapter_id FROM scenes`)
	if err != nil {
		return nil, nil, fmt.Errorf("index entities jsonl: %w", err)
	}
	for rows.Next() {
		var id, chapterID string
		if err := rows.Scan(&id, &chapterID); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("index entities jsonl: %w", err)
		}
		sceneChapterByID[id] = chapterID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("index entities jsonl: %w", err)
	}
	return chapterIDs, sceneChapterByID, nil
}

func readCommittedEntityJSONL(
	path string,
	chapterIDs map[string]bool,
	sceneChapterByID map[string]string,
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
			chapterID, current, err := entityRecordChapter(path, lineNo, rec, chapterIDs, sceneChapterByID)
			if err != nil {
				return err
			}
			if !current {
				return nil
			}
			rec.ChapterID = chapterID
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
			if snap.OccurrenceCount == nil {
				if snap.MentionCount != nil {
					return nil
				}
				return fmt.Errorf("index entities jsonl: %s:%d: entity_snapshot missing occurrence_count", path, lineNo)
			}
			if *snap.EntityCount < 0 {
				return fmt.Errorf("index entities jsonl: %s:%d: entity_snapshot has invalid entity_count %d", path, lineNo, *snap.EntityCount)
			}
			if *snap.OccurrenceCount < 0 {
				return fmt.Errorf("index entities jsonl: %s:%d: entity_snapshot has invalid occurrence_count %d", path, lineNo, *snap.OccurrenceCount)
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

func entityRecordChapter(path string, lineNo int, rec entityJSONLRecord, chapterIDs map[string]bool, sceneChapterByID map[string]string) (string, bool, error) {
	chapterID := strings.TrimSpace(rec.ChapterID)
	if len(rec.Evidence) == 0 {
		if chapterID == "" {
			return "", false, nil
		}
		return "", false, fmt.Errorf("index entities jsonl: %s:%d: entity %s has no scene evidence", path, lineNo, rec.ID)
	}
	inferredChapterID := ""
	for _, sceneID := range rec.Evidence {
		sceneID = strings.TrimSpace(sceneID)
		if sceneID == "" {
			return "", false, fmt.Errorf("index entities jsonl: %s:%d: entity %s has blank scene evidence", path, lineNo, rec.ID)
		}
		sceneChapter, ok := sceneChapterByID[sceneID]
		if !ok {
			if chapterID == "" {
				return "", false, nil
			}
			return "", false, fmt.Errorf("index entities jsonl: %s:%d: entity %s references missing evidence scene %q", path, lineNo, rec.ID, sceneID)
		}
		if inferredChapterID == "" {
			inferredChapterID = sceneChapter
		} else if inferredChapterID != sceneChapter {
			return "", false, fmt.Errorf("index entities jsonl: %s:%d: entity %s evidence spans chapters %s and %s", path, lineNo, rec.ID, inferredChapterID, sceneChapter)
		}
	}
	if chapterID == "" {
		return inferredChapterID, true, nil
	}
	if !chapterIDs[chapterID] {
		return "", false, fmt.Errorf("index entities jsonl: %s:%d: entity %s references missing chapter %q", path, lineNo, rec.ID, chapterID)
	}
	if inferredChapterID != chapterID {
		return "", false, fmt.Errorf("index entities jsonl: %s:%d: entity %s evidence is in chapter %s, not %s", path, lineNo, rec.ID, inferredChapterID, chapterID)
	}
	return chapterID, true, nil
}

func readCommittedOccurrenceJSONL(
	path string,
	sceneChapterByID map[string]string,
	entityChapterByID map[string]string,
	allEntityIDs map[string]bool,
) (map[string]occurrenceCandidate, map[string]int, error) {
	out := make(map[string]occurrenceCandidate)
	if err := scanJSONL(path, func(lineNo int, line []byte) error {
		var typed struct {
			RecordType string `json:"record_type"`
		}
		if err := json.Unmarshal(line, &typed); err != nil {
			return fmt.Errorf("index occurrences jsonl: %s:%d: malformed json: %w", path, lineNo, err)
		}
		if typed.RecordType != "occurrence" {
			return nil
		}
		var rec occurrenceJSONLRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return fmt.Errorf("index occurrences jsonl: %s:%d: malformed occurrence record: %w", path, lineNo, err)
		}
		entityID := strings.TrimSpace(rec.EntityID)
		entityChapter, committed := entityChapterByID[entityID]
		if !committed {
			if allEntityIDs[entityID] {
				return nil
			}
			return fmt.Errorf("index occurrences jsonl: %s:%d: occurrence references missing entity %q", path, lineNo, rec.EntityID)
		}
		sceneID := strings.TrimSpace(rec.SceneID)
		chapterID, ok := sceneChapterByID[sceneID]
		if !ok {
			return fmt.Errorf("index occurrences jsonl: %s:%d: occurrence references missing scene %q", path, lineNo, rec.SceneID)
		}
		if strings.TrimSpace(rec.ChapterID) == "" {
			rec.ChapterID = chapterID
		} else if rec.ChapterID != chapterID {
			return fmt.Errorf("index occurrences jsonl: %s:%d: occurrence chapter %s does not match scene chapter %s", path, lineNo, rec.ChapterID, chapterID)
		}
		if entityChapter != chapterID {
			return fmt.Errorf("index occurrences jsonl: %s:%d: occurrence chapter %s does not match entity chapter %s", path, lineNo, chapterID, entityChapter)
		}
		rec.SurfaceTexts = cleanStringList(rec.SurfaceTexts)
		rec.SourceFields = cleanStringList(rec.SourceFields)
		if len(rec.SurfaceTexts) == 0 {
			return fmt.Errorf("index occurrences jsonl: %s:%d: occurrence %s/%s has no surface_texts", path, lineNo, rec.EntityID, rec.SceneID)
		}
		key := rec.EntityID + "\x00" + rec.SceneID
		out[key] = occurrenceCandidate{record: rec, line: lineNo, raw: string(line)}
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

func cleanStringList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func sortedEntityIDs(values map[string]entityCandidate) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedOccurrenceKeys(values map[string]occurrenceCandidate) []string {
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
