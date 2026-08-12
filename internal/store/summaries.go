package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SummaryCharacterFinalState is one principal-character final state carried by a book summary.
type SummaryCharacterFinalState struct {
	CharacterID string `json:"character_id"`
	State       string `json:"state"`
}

// SummaryRow is a generated summary record read from the rebuildable index.
type SummaryRow struct {
	RecordID             string                       `json:"record_id"`
	RecordType           string                       `json:"record_type"`
	ChapterID            string                       `json:"chapter_id,omitempty"`
	ChapterTitle         string                       `json:"chapter_title,omitempty"`
	Summary              string                       `json:"summary"`
	Themes               []string                     `json:"themes,omitempty"`
	Unresolved           []string                     `json:"unresolved,omitempty"`
	Evidence             []string                     `json:"evidence"`
	SourceRecords        []string                     `json:"source_records,omitempty"`
	CharacterFinalStates []SummaryCharacterFinalState `json:"character_final_states,omitempty"`
	GenerationRun        string                       `json:"generation_run,omitempty"`
	GenerationModel      string                       `json:"generation_model,omitempty"`
	PromptVersion        string                       `json:"prompt_version,omitempty"`
	GeneratedAt          string                       `json:"generated_at,omitempty"`
	CharacterRolesHash   string                       `json:"character_roles_hash,omitempty"`
	Status               string                       `json:"status"`
	RawJSON              string                       `json:"raw_json,omitempty"`
}

type summaryJSONLRecord struct {
	RecordType           string                       `json:"record_type"`
	ChapterID            string                       `json:"chapter_id"`
	ChapterTitle         string                       `json:"chapter_title"`
	Summary              string                       `json:"summary"`
	Themes               []string                     `json:"themes"`
	Unresolved           []string                     `json:"unresolved"`
	Evidence             []string                     `json:"evidence"`
	SourceRecords        []string                     `json:"source_records"`
	CharacterFinalStates []SummaryCharacterFinalState `json:"character_final_states"`
	Generation           struct {
		RunID              string `json:"run_id"`
		Model              string `json:"model"`
		PromptVersion      string `json:"prompt_version"`
		GeneratedAt        string `json:"generated_at"`
		CharacterRolesHash string `json:"character_roles_hash"`
	} `json:"generation"`
	Status string `json:"status"`
}

// SummaryRecordID returns the stable operational record ID for a summary row.
func SummaryRecordID(recordType, chapterID string) string {
	switch strings.TrimSpace(recordType) {
	case "book_summary":
		return "book_summary"
	case "chapter_summary":
		chapterID = strings.TrimSpace(chapterID)
		if chapterID != "" {
			return "chapter_summary:" + chapterID
		}
	}
	return ""
}

// InsertSummary inserts or replaces one indexed summary row and its FTS entry.
func (s *Store) InsertSummary(r SummaryRow) (retErr error) {
	r, err := normalizeSummaryRow(r)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("insert summary %s: %w", r.RecordID, err)
	}
	defer func() {
		if retErr != nil {
			tx.Rollback()
		}
	}()
	if err := insertSummaryTx(tx, r); err != nil {
		return err
	}
	return tx.Commit()
}

// InspectSummary returns a summary by target. Target may be book, book_summary,
// chapter_summary:<chapter-id>, or a chapter ID.
func (s *Store) InspectSummary(target string) (SummaryRow, error) {
	recordID := summaryTargetRecordID(target)
	if recordID == "" {
		return SummaryRow{}, fmt.Errorf("summary %s: %w", target, ErrNotFound)
	}
	row, err := s.inspectSummaryByRecordID(recordID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return SummaryRow{}, fmt.Errorf("summary %s: %w", target, ErrNotFound)
		}
		return SummaryRow{}, err
	}
	return row, nil
}

// SummaryRowsForChapter returns indexed summaries for ask context. With a
// chapter ID it returns that chapter summary only. Without one it returns the
// book summary followed by chapter summaries in manuscript order.
func (s *Store) SummaryRowsForChapter(chapterID string) ([]SummaryRow, error) {
	chapterID = strings.TrimSpace(chapterID)
	query := `SELECT s.record_id, s.record_type, s.chapter_id, s.chapter_title, s.summary,
		        s.themes_json, s.unresolved_json, s.evidence_json, s.source_records_json,
		        s.character_final_states_json, s.generation_run, s.generation_model,
		        s.prompt_version, s.generated_at, s.character_roles_hash, s.status, s.raw_json
		 FROM summaries s
		 LEFT JOIN chapters c ON c.id = s.chapter_id`
	var args []any
	if chapterID != "" {
		query += ` WHERE s.record_type = 'chapter_summary' AND s.chapter_id = ?`
		args = append(args, chapterID)
	}
	query += ` ORDER BY CASE s.record_type WHEN 'book_summary' THEN 0 ELSE 1 END,
		          COALESCE(c.ordinal, 999999), s.chapter_id, s.record_id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list summaries: %w", err)
	}
	defer rows.Close()
	return scanSummaryRows(rows)
}

// SummaryCounts returns indexed summary counts.
func (s *Store) SummaryCounts() (summaries, chapterSummaries, bookSummaries int, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM summaries`).Scan(&summaries); err != nil {
		return 0, 0, 0, fmt.Errorf("count summaries: %w", err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM summaries WHERE record_type = 'chapter_summary'`).Scan(&chapterSummaries); err != nil {
		return 0, 0, 0, fmt.Errorf("count chapter summaries: %w", err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM summaries WHERE record_type = 'book_summary'`).Scan(&bookSummaries); err != nil {
		return 0, 0, 0, fmt.Errorf("count book summaries: %w", err)
	}
	return summaries, chapterSummaries, bookSummaries, nil
}

// SearchSummaries returns summaries whose summary, themes, or unresolved text
// matches query. If chapterID is non-empty, only that chapter summary is searched.
func (s *Store) SearchSummaries(query, chapterID string, limit int) ([]SummaryRow, error) {
	if limit <= 0 {
		limit = 10
	}
	q := sanitizeFTSQuery(query)
	if q == "" {
		return nil, nil
	}
	join := `JOIN summaries s ON s.record_id = f.record_id`
	where := `summaries_fts MATCH ?`
	args := []any{q}
	if strings.TrimSpace(chapterID) != "" {
		where += ` AND s.chapter_id = ?`
		args = append(args, strings.TrimSpace(chapterID))
	}
	args = append(args, limit)
	ids, err := s.queryFTSIDs(
		`SELECT f.record_id
		 FROM summaries_fts f
		 `+join+`
		 WHERE `+where+`
		 ORDER BY rank LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("search summaries: %w", err)
	}
	out := make([]SummaryRow, 0, len(ids))
	for _, id := range ids {
		row, err := s.inspectSummaryByRecordID(id)
		if err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// IndexSummariesJSONL replays model/summaries.jsonl into the summary projection.
// The latest valid record for each derived summary record ID wins.
func (s *Store) IndexSummariesJSONL(path string) (retErr error) {
	chapterIDs, paragraphChapterByID, err := s.summaryReplayRefs()
	if err != nil {
		return err
	}
	latest, err := readLatestSummaryRows(path, chapterIDs, paragraphChapterByID)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("index summaries jsonl: %w", err)
	}
	defer func() {
		if retErr != nil {
			tx.Rollback()
		}
	}()
	for _, stmt := range []string{`DELETE FROM summaries_fts`, `DELETE FROM summaries`} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("index summaries jsonl: %w", err)
		}
	}
	ids := sortedSummaryRecordIDs(latest)
	for _, id := range ids {
		if err := insertSummaryTx(tx, latest[id]); err != nil {
			return fmt.Errorf("index summaries jsonl: %w", err)
		}
	}
	return tx.Commit()
}
func (s *Store) inspectSummaryByRecordID(recordID string) (SummaryRow, error) {
	rows, err := s.db.Query(
		`SELECT record_id, record_type, chapter_id, chapter_title, summary,
		        themes_json, unresolved_json, evidence_json, source_records_json,
		        character_final_states_json, generation_run, generation_model,
		        prompt_version, generated_at, character_roles_hash, status, raw_json
		 FROM summaries WHERE record_id = ?`,
		recordID,
	)
	if err != nil {
		return SummaryRow{}, fmt.Errorf("inspect summary %s: %w", recordID, err)
	}
	defer rows.Close()
	out, err := scanSummaryRows(rows)
	if err != nil {
		return SummaryRow{}, fmt.Errorf("inspect summary %s: %w", recordID, err)
	}
	if len(out) == 0 {
		return SummaryRow{}, ErrNotFound
	}
	return out[0], nil
}

func insertSummaryTx(tx *sql.Tx, r SummaryRow) error {
	r, err := normalizeSummaryRow(r)
	if err != nil {
		return err
	}
	themesJSON, err := json.Marshal(r.Themes)
	if err != nil {
		return fmt.Errorf("marshal themes for summary %s: %w", r.RecordID, err)
	}
	unresolvedJSON, err := json.Marshal(r.Unresolved)
	if err != nil {
		return fmt.Errorf("marshal unresolved for summary %s: %w", r.RecordID, err)
	}
	evidenceJSON, err := json.Marshal(r.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence for summary %s: %w", r.RecordID, err)
	}
	sourceRecordsJSON, err := json.Marshal(r.SourceRecords)
	if err != nil {
		return fmt.Errorf("marshal source records for summary %s: %w", r.RecordID, err)
	}
	finalStatesJSON, err := json.Marshal(r.CharacterFinalStates)
	if err != nil {
		return fmt.Errorf("marshal final states for summary %s: %w", r.RecordID, err)
	}
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO summaries
			(record_id, record_type, chapter_id, chapter_title, summary, themes_json,
			 unresolved_json, evidence_json, source_records_json, character_final_states_json,
			 generation_run, generation_model, prompt_version, generated_at,
			 character_roles_hash, status, raw_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.RecordID, r.RecordType, nullableString(r.ChapterID), r.ChapterTitle, r.Summary,
		string(themesJSON), string(unresolvedJSON), string(evidenceJSON), string(sourceRecordsJSON),
		string(finalStatesJSON), r.GenerationRun, r.GenerationModel, r.PromptVersion, r.GeneratedAt,
		r.CharacterRolesHash, r.Status, r.RawJSON,
	); err != nil {
		return fmt.Errorf("insert summary %s: %w", r.RecordID, err)
	}
	if _, err := tx.Exec(`DELETE FROM summaries_fts WHERE record_id = ?`, r.RecordID); err != nil {
		return fmt.Errorf("delete summary FTS %s: %w", r.RecordID, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO summaries_fts(record_id, summary, themes, unresolved) VALUES (?, ?, ?, ?)`,
		r.RecordID, r.Summary, strings.Join(r.Themes, " "), strings.Join(r.Unresolved, " "),
	); err != nil {
		return fmt.Errorf("index summary FTS %s: %w", r.RecordID, err)
	}
	return nil
}

func scanSummaryRows(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]SummaryRow, error) {
	var out []SummaryRow
	for rows.Next() {
		var r SummaryRow
		var chapterID sql.NullString
		var themesJSON, unresolvedJSON, evidenceJSON, sourceRecordsJSON, finalStatesJSON string
		if err := rows.Scan(&r.RecordID, &r.RecordType, &chapterID, &r.ChapterTitle, &r.Summary,
			&themesJSON, &unresolvedJSON, &evidenceJSON, &sourceRecordsJSON, &finalStatesJSON,
			&r.GenerationRun, &r.GenerationModel, &r.PromptVersion, &r.GeneratedAt,
			&r.CharacterRolesHash, &r.Status, &r.RawJSON); err != nil {
			return nil, err
		}
		if chapterID.Valid {
			r.ChapterID = chapterID.String
		}
		if err := json.Unmarshal([]byte(themesJSON), &r.Themes); err != nil {
			return nil, fmt.Errorf("parse themes for summary %s: %w", r.RecordID, err)
		}
		if err := json.Unmarshal([]byte(unresolvedJSON), &r.Unresolved); err != nil {
			return nil, fmt.Errorf("parse unresolved for summary %s: %w", r.RecordID, err)
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &r.Evidence); err != nil {
			return nil, fmt.Errorf("parse evidence for summary %s: %w", r.RecordID, err)
		}
		if err := json.Unmarshal([]byte(sourceRecordsJSON), &r.SourceRecords); err != nil {
			return nil, fmt.Errorf("parse source records for summary %s: %w", r.RecordID, err)
		}
		if err := json.Unmarshal([]byte(finalStatesJSON), &r.CharacterFinalStates); err != nil {
			return nil, fmt.Errorf("parse final states for summary %s: %w", r.RecordID, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func normalizeSummaryRow(r SummaryRow) (SummaryRow, error) {
	r.RecordType = strings.TrimSpace(r.RecordType)
	r.ChapterID = strings.TrimSpace(r.ChapterID)
	if r.RecordID == "" {
		r.RecordID = SummaryRecordID(r.RecordType, r.ChapterID)
	}
	r.RecordID = strings.TrimSpace(r.RecordID)
	if r.RecordID == "" {
		return SummaryRow{}, fmt.Errorf("summary missing record_id")
	}
	if r.RecordType == "chapter_summary" && r.ChapterID == "" {
		return SummaryRow{}, fmt.Errorf("summary %s missing chapter_id", r.RecordID)
	}
	if r.RecordType != "chapter_summary" && r.RecordType != "book_summary" {
		return SummaryRow{}, fmt.Errorf("summary %s has unsupported record_type %q", r.RecordID, r.RecordType)
	}
	if strings.TrimSpace(r.Summary) == "" {
		return SummaryRow{}, fmt.Errorf("summary %s missing summary", r.RecordID)
	}
	if strings.TrimSpace(r.Status) == "" {
		r.Status = "generated"
	}
	r.Themes = cleanStringList(r.Themes)
	r.Unresolved = cleanStringList(r.Unresolved)
	r.Evidence = cleanStringList(r.Evidence)
	r.SourceRecords = cleanStringList(r.SourceRecords)
	r.CharacterFinalStates = cleanSummaryFinalStates(r.CharacterFinalStates)
	return r, nil
}

func cleanSummaryFinalStates(values []SummaryCharacterFinalState) []SummaryCharacterFinalState {
	seen := make(map[string]bool, len(values))
	out := make([]SummaryCharacterFinalState, 0, len(values))
	for _, value := range values {
		characterID := strings.TrimSpace(value.CharacterID)
		state := strings.TrimSpace(value.State)
		if characterID == "" || state == "" || seen[characterID] {
			continue
		}
		seen[characterID] = true
		out = append(out, SummaryCharacterFinalState{CharacterID: characterID, State: state})
	}
	return out
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func summaryTargetRecordID(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if target == "book" || target == "book_summary" {
		return "book_summary"
	}
	if strings.HasPrefix(target, "chapter_summary:") {
		return target
	}
	return "chapter_summary:" + target
}

func (s *Store) summaryReplayRefs() (map[string]bool, map[string]string, error) {
	chapterIDs := make(map[string]bool)
	chapterRows, err := s.db.Query(`SELECT id FROM chapters`)
	if err != nil {
		return nil, nil, fmt.Errorf("index summaries jsonl: %w", err)
	}
	for chapterRows.Next() {
		var id string
		if err := chapterRows.Scan(&id); err != nil {
			chapterRows.Close()
			return nil, nil, fmt.Errorf("index summaries jsonl: %w", err)
		}
		chapterIDs[id] = true
	}
	chapterRows.Close()
	if err := chapterRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("index summaries jsonl: %w", err)
	}

	paragraphChapterByID := make(map[string]string)
	rows, err := s.db.Query(`SELECT id, chapter_id FROM paragraphs`)
	if err != nil {
		return nil, nil, fmt.Errorf("index summaries jsonl: %w", err)
	}
	for rows.Next() {
		var id, chapterID string
		if err := rows.Scan(&id, &chapterID); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("index summaries jsonl: %w", err)
		}
		paragraphChapterByID[id] = chapterID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("index summaries jsonl: %w", err)
	}
	return chapterIDs, paragraphChapterByID, nil
}
func readLatestSummaryRows(path string, chapterIDs map[string]bool, paragraphChapterByID map[string]string) (map[string]SummaryRow, error) {
	latest := make(map[string]SummaryRow)
	if err := scanJSONL(path, func(lineNo int, line []byte) error {
		var typed struct {
			RecordType string `json:"record_type"`
		}
		if err := json.Unmarshal(line, &typed); err != nil {
			return fmt.Errorf("index summaries jsonl: %s:%d: malformed json: %w", path, lineNo, err)
		}
		switch typed.RecordType {
		case "chapter_summary", "book_summary":
			var rec summaryJSONLRecord
			if err := json.Unmarshal(line, &rec); err != nil {
				return fmt.Errorf("index summaries jsonl: %s:%d: malformed summary record: %w", path, lineNo, err)
			}
			row, err := summaryRowFromJSONL(path, lineNo, rec, string(line), chapterIDs, paragraphChapterByID)
			if err != nil {
				return err
			}
			latest[row.RecordID] = row
		default:
			// Ignore unsupported record types for forward compatibility.
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return latest, nil
}

func summaryRowFromJSONL(path string, lineNo int, rec summaryJSONLRecord, raw string, chapterIDs map[string]bool, paragraphChapterByID map[string]string) (SummaryRow, error) {
	recordType := strings.TrimSpace(rec.RecordType)
	chapterID := strings.TrimSpace(rec.ChapterID)
	row := SummaryRow{
		RecordID:             SummaryRecordID(recordType, chapterID),
		RecordType:           recordType,
		ChapterID:            chapterID,
		ChapterTitle:         strings.TrimSpace(rec.ChapterTitle),
		Summary:              strings.TrimSpace(rec.Summary),
		Themes:               cleanStringList(rec.Themes),
		Unresolved:           cleanStringList(rec.Unresolved),
		Evidence:             cleanStringList(rec.Evidence),
		SourceRecords:        cleanStringList(rec.SourceRecords),
		CharacterFinalStates: cleanSummaryFinalStates(rec.CharacterFinalStates),
		GenerationRun:        strings.TrimSpace(rec.Generation.RunID),
		GenerationModel:      strings.TrimSpace(rec.Generation.Model),
		PromptVersion:        strings.TrimSpace(rec.Generation.PromptVersion),
		GeneratedAt:          strings.TrimSpace(rec.Generation.GeneratedAt),
		CharacterRolesHash:   strings.TrimSpace(rec.Generation.CharacterRolesHash),
		Status:               strings.TrimSpace(rec.Status),
		RawJSON:              raw,
	}
	if row.Status == "" {
		row.Status = "generated"
	}
	if row.Summary == "" {
		return SummaryRow{}, fmt.Errorf("index summaries jsonl: %s:%d: %s missing summary", path, lineNo, recordType)
	}
	switch recordType {
	case "chapter_summary":
		if chapterID == "" {
			return SummaryRow{}, fmt.Errorf("index summaries jsonl: %s:%d: chapter_summary missing chapter_id", path, lineNo)
		}
		if !chapterIDs[chapterID] {
			return SummaryRow{}, fmt.Errorf("index summaries jsonl: %s:%d: chapter_summary references missing chapter %q", path, lineNo, chapterID)
		}
		for _, pid := range row.Evidence {
			pidChapter, ok := paragraphChapterByID[pid]
			if !ok {
				return SummaryRow{}, fmt.Errorf("index summaries jsonl: %s:%d: chapter_summary %s references missing evidence paragraph %q", path, lineNo, chapterID, pid)
			}
			if pidChapter != chapterID {
				return SummaryRow{}, fmt.Errorf("index summaries jsonl: %s:%d: chapter_summary %s evidence paragraph %q is in chapter %s", path, lineNo, chapterID, pid, pidChapter)
			}
		}
	case "book_summary":
		if chapterID != "" {
			return SummaryRow{}, fmt.Errorf("index summaries jsonl: %s:%d: book_summary must not have chapter_id", path, lineNo)
		}
		for _, id := range append(append([]string{}, row.Evidence...), row.SourceRecords...) {
			if !chapterIDs[id] {
				return SummaryRow{}, fmt.Errorf("index summaries jsonl: %s:%d: book_summary references missing chapter %q", path, lineNo, id)
			}
		}
	default:
		return SummaryRow{}, fmt.Errorf("index summaries jsonl: %s:%d: unsupported summary record_type %q", path, lineNo, recordType)
	}
	return normalizeSummaryRow(row)
}

func sortedSummaryRecordIDs(values map[string]SummaryRow) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
