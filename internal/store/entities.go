package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

// DeleteEntityMentionsForChapter removes indexed entity mentions for a chapter
// and then drops entities that no longer have any mentions.
func (s *Store) DeleteEntityMentionsForChapter(chapterID string) error {
	_, err := s.db.Exec(`DELETE FROM mentions WHERE chapter_id = ?`, chapterID)
	if err != nil {
		return fmt.Errorf("delete entity mentions for chapter %s: %w", chapterID, err)
	}
	_, err = s.db.Exec(`DELETE FROM entities WHERE id NOT IN (SELECT DISTINCT entity_id FROM mentions)`)
	if err != nil {
		return fmt.Errorf("delete orphan entities after chapter %s: %w", chapterID, err)
	}
	return nil
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

// IndexEntitiesJSONL replays model/entities.jsonl and model/mentions.jsonl into the index.
func (s *Store) IndexEntitiesJSONL(entitiesPath, mentionsPath string) (retErr error) {
	paragraphs := make(map[string]string)
	rows, err := s.db.Query(`SELECT id, chapter_id FROM paragraphs`)
	if err != nil {
		return fmt.Errorf("index entities jsonl: %w", err)
	}
	for rows.Next() {
		var id, chapterID string
		if err := rows.Scan(&id, &chapterID); err != nil {
			rows.Close()
			return fmt.Errorf("index entities jsonl: %w", err)
		}
		paragraphs[id] = chapterID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("index entities jsonl: %w", err)
	}

	entities, err := readEntityJSONL(entitiesPath)
	if err != nil {
		return err
	}
	mentions, err := readMentionJSONL(mentionsPath)
	if err != nil {
		return err
	}
	for id, cand := range entities {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("index entities jsonl: %s:%d: entity missing id", entitiesPath, cand.line)
		}
		for _, pid := range cand.record.Evidence {
			if _, ok := paragraphs[pid]; !ok {
				return fmt.Errorf("index entities jsonl: %s:%d: entity %s references missing evidence paragraph %q", entitiesPath, cand.line, id, pid)
			}
		}
	}
	for key, cand := range mentions {
		_ = key
		if _, ok := entities[cand.record.EntityID]; !ok {
			return fmt.Errorf("index mentions jsonl: %s:%d: mention references missing entity %q", mentionsPath, cand.line, cand.record.EntityID)
		}
		chapterID, ok := paragraphs[cand.record.ParagraphID]
		if !ok {
			return fmt.Errorf("index mentions jsonl: %s:%d: mention references missing paragraph %q", mentionsPath, cand.line, cand.record.ParagraphID)
		}
		if cand.record.ChapterID == "" {
			cand.record.ChapterID = chapterID
			mentions[key] = cand
		} else if cand.record.ChapterID != chapterID {
			return fmt.Errorf("index mentions jsonl: %s:%d: mention chapter %s does not match paragraph chapter %s", mentionsPath, cand.line, cand.record.ChapterID, chapterID)
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
	for _, stmt := range []string{`DELETE FROM mentions`, `DELETE FROM entities`} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("index entities jsonl: %w", err)
		}
	}
	for _, cand := range entities {
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
	for _, cand := range mentions {
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
	return tx.Commit()
}

func readEntityJSONL(path string) (map[string]entityCandidate, error) {
	out := make(map[string]entityCandidate)
	if err := scanJSONL(path, func(lineNo int, line []byte) error {
		var typed struct {
			RecordType string `json:"record_type"`
		}
		if err := json.Unmarshal(line, &typed); err != nil {
			return fmt.Errorf("index entities jsonl: %s:%d: malformed json: %w", path, lineNo, err)
		}
		if typed.RecordType != "entity" {
			return nil
		}
		var rec entityJSONLRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return fmt.Errorf("index entities jsonl: %s:%d: malformed entity record: %w", path, lineNo, err)
		}
		out[rec.ID] = entityCandidate{record: rec, line: lineNo, raw: string(line)}
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func readMentionJSONL(path string) (map[string]mentionCandidate, error) {
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
		key := rec.EntityID + "\x00" + rec.ParagraphID + "\x00" + rec.SurfaceText
		out[key] = mentionCandidate{record: rec, line: lineNo, raw: string(line)}
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
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
