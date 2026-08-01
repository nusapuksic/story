package store

import (
	"fmt"
	"strings"
	"unicode"
)

// SceneCardStatusPolicy controls which scene-card statuses are eligible for
// retrieval context.
type SceneCardStatusPolicy string

const (
	// SceneCardStatusTrustedOnly includes only reviewed, supported card states.
	SceneCardStatusTrustedOnly SceneCardStatusPolicy = "trusted-only"
	// SceneCardStatusIncludeGenerated includes generated cards plus trusted states.
	SceneCardStatusIncludeGenerated SceneCardStatusPolicy = "include-generated"
)

// SearchParagraphs returns paragraphs whose text matches query using SQLite
// FTS5.  If chapterID is non-empty, results are filtered to that chapter.
// limit controls the maximum number of results; 0 uses the default (20).
// A malformed FTS query returns an empty slice rather than an error.
func (s *Store) SearchParagraphs(query, chapterID string, limit int) ([]ParagraphRow, error) {
	if limit <= 0 {
		limit = 20
	}
	q := sanitizeFTSQuery(query)
	if q == "" {
		return nil, nil
	}

	var (
		ids []string
		err error
	)
	if chapterID != "" {
		ids, err = s.queryFTSIDs(
			`SELECT id FROM paragraphs_fts WHERE text MATCH ? AND chapter_id = ? ORDER BY rank LIMIT ?`,
			q, chapterID, limit,
		)
	} else {
		ids, err = s.queryFTSIDs(
			`SELECT id FROM paragraphs_fts WHERE text MATCH ? ORDER BY rank LIMIT ?`,
			q, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("search paragraphs: %w", err)
	}

	var out []ParagraphRow
	for _, id := range ids {
		p, err := s.InspectParagraph(id)
		if err != nil {
			continue // paragraph may have been deleted since FTS was last synced
		}
		out = append(out, p)
	}
	return out, nil
}

// SearchSceneCards returns scene cards whose title or summary matches query
// using SQLite FTS5.  limit controls the maximum number of results; 0 uses
// the default (20).  A malformed FTS query returns an empty slice.
func (s *Store) SearchSceneCards(query string, limit int) ([]SceneCardRow, error) {
	return s.SearchSceneCardsByStatusPolicy(query, SceneCardStatusIncludeGenerated, limit)
}

// SearchSceneCardsByStatusPolicy returns scene cards matching query whose
// status is eligible under policy.
func (s *Store) SearchSceneCardsByStatusPolicy(query string, policy SceneCardStatusPolicy, limit int) ([]SceneCardRow, error) {
	if limit <= 0 {
		limit = 20
	}
	q := sanitizeFTSQuery(query)
	if q == "" {
		return nil, nil
	}

	statuses := allowedSceneCardStatuses(policy)
	sceneIDs, err := s.queryFTSIDs(
		`SELECT f.scene_id
		 FROM scene_cards_fts f
		 JOIN scene_cards sc ON sc.scene_id = f.scene_id
		 WHERE scene_cards_fts MATCH ? AND lower(sc.status) IN (`+placeholders(len(statuses))+`)
		 ORDER BY rank LIMIT ?`,
		append(append([]any{q}, stringsToAny(statuses)...), limit)...,
	)
	if err != nil {
		return nil, fmt.Errorf("search scene cards: %w", err)
	}

	var out []SceneCardRow
	for _, id := range sceneIDs {
		card, err := s.InspectSceneCard(id)
		if err != nil {
			continue
		}
		out = append(out, card)
	}
	return out, nil
}

func filterSceneCardsByStatusPolicy(cards []SceneCardRow, policy SceneCardStatusPolicy) []SceneCardRow {
	if len(cards) == 0 {
		return nil
	}
	out := make([]SceneCardRow, 0, len(cards))
	for _, card := range cards {
		if sceneCardStatusAllowed(policy, card.Status) {
			out = append(out, card)
		}
	}
	return out
}

func sceneCardStatusAllowed(policy SceneCardStatusPolicy, status string) bool {
	status = strings.TrimSpace(strings.ToLower(status))
	for _, allowed := range allowedSceneCardStatuses(policy) {
		if status == allowed {
			return true
		}
	}
	return false
}

func allowedSceneCardStatuses(policy SceneCardStatusPolicy) []string {
	switch SceneCardStatusPolicy(strings.TrimSpace(strings.ToLower(string(policy)))) {
	case SceneCardStatusIncludeGenerated:
		return []string{"generated", "verified", "accepted"}
	default:
		return []string{"verified", "accepted"}
	}
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func stringsToAny(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

// queryFTSIDs runs an FTS query and returns the first column of each result
// row as a string slice.  FTS syntax errors are treated as "no results".
func (s *Store) queryFTSIDs(query string, args ...any) ([]string, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		if isFTSQueryError(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// sanitizeFTSQuery converts a free-text user query into a safe FTS5 MATCH
// expression.  It keeps letters and digits; replaces everything else with
// spaces; then trims and collapses whitespace.
func sanitizeFTSQuery(q string) string {
	var b strings.Builder
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	words := strings.Fields(b.String())
	return strings.Join(words, " ")
}

// isFTSQueryError returns true when err is a syntax error from an FTS5 MATCH
// expression, allowing callers to treat bad queries as "no results" rather
// than propagating the error.
func isFTSQueryError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "fts5") || strings.Contains(msg, "syntax error")
}
