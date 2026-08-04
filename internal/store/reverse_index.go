package store

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"sync"
)

const (
	ReverseTermTheme       = "theme"
	ReverseTermEntity      = "entity"
	ReverseTermParticipant = "participant"
	ReverseTermPOV         = "pov"
	ReverseTermLocation    = "location"
	ReverseTermUnresolved  = "unresolved"
)

type ReverseIndexTerm struct {
	TermType        string `json:"term_type"`
	Term            string `json:"term"`
	OccurrenceCount int    `json:"occurrence_count"`
}

type ReverseIndexRef struct {
	TermType    string  `json:"term_type"`
	Term        string  `json:"term"`
	SceneID     string  `json:"scene_id"`
	ChapterID   string  `json:"chapter_id"`
	SourceField string  `json:"source_field"`
	Weight      float64 `json:"weight"`
	RawValue    string  `json:"raw_value"`
}

type reverseIndexSourceCard struct {
	SceneID   string
	ChapterID string
	RawJSON   string
}

type reverseTermSource struct {
	termType string
	field    string
}

var reverseTermSources = []reverseTermSource{
	{termType: ReverseTermTheme, field: "themes"},
	{termType: ReverseTermEntity, field: "entities"},
	{termType: ReverseTermParticipant, field: "participants"},
	{termType: ReverseTermPOV, field: "pov"},
	{termType: ReverseTermLocation, field: "locations"},
	{termType: ReverseTermUnresolved, field: "unresolved"},
}

// RebuildReverseIndex rebuilds the derived reverse index from current scene-card rows.
func (s *Store) RebuildReverseIndex() (retErr error) {
	cards, err := s.reverseIndexSourceCards()
	if err != nil {
		return err
	}
	refs, terms := reverseIndexRowsFromCards(cards)

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("rebuild reverse index: %w", err)
	}
	defer func() {
		if retErr != nil {
			tx.Rollback()
		}
	}()
	for _, stmt := range []string{
		`DELETE FROM reverse_index_refs`,
		`DELETE FROM reverse_index_terms`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("rebuild reverse index: %w", err)
		}
	}
	insertTermStmt, err := tx.Prepare(
		`INSERT INTO reverse_index_terms (term_type, term, occurrence_count) VALUES (?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("rebuild reverse index: prepare term insert: %w", err)
	}
	defer insertTermStmt.Close()
	insertRefStmt, err := tx.Prepare(
		`INSERT INTO reverse_index_refs (term_type, term, scene_id, chapter_id, source_field, weight, raw_value)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("rebuild reverse index: prepare ref insert: %w", err)
	}
	defer insertRefStmt.Close()

	for _, term := range terms {
		if _, err := insertTermStmt.Exec(
			term.TermType, term.Term, term.OccurrenceCount,
		); err != nil {
			return fmt.Errorf("rebuild reverse index: insert term %s/%s: %w", term.TermType, term.Term, err)
		}
	}
	for _, ref := range refs {
		if _, err := insertRefStmt.Exec(
			ref.TermType, ref.Term, ref.SceneID, ref.ChapterID, ref.SourceField, ref.Weight, ref.RawValue,
		); err != nil {
			return fmt.Errorf("rebuild reverse index: insert ref %s/%s/%s: %w", ref.TermType, ref.Term, ref.SceneID, err)
		}
	}
	return tx.Commit()
}

func (s *Store) reverseIndexSourceCards() ([]reverseIndexSourceCard, error) {
	rows, err := s.db.Query(
		`SELECT sc.scene_id, sn.chapter_id, sc.raw_json
		 FROM scene_cards sc
		 JOIN scenes sn ON sn.id = sc.scene_id
		 JOIN chapters c ON c.id = sn.chapter_id
		 ORDER BY c.ordinal, sn.ordinal`,
	)
	if err != nil {
		return nil, fmt.Errorf("reverse index source cards: %w", err)
	}
	defer rows.Close()

	var cards []reverseIndexSourceCard
	for rows.Next() {
		var card reverseIndexSourceCard
		if err := rows.Scan(&card.SceneID, &card.ChapterID, &card.RawJSON); err != nil {
			return nil, fmt.Errorf("reverse index source cards: %w", err)
		}
		cards = append(cards, card)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reverse index source cards: %w", err)
	}
	return cards, nil
}

func reverseIndexRowsFromCards(cards []reverseIndexSourceCard) ([]ReverseIndexRef, []ReverseIndexTerm) {
	cardRefs := reverseIndexRefsFromCards(cards)
	seenRef := make(map[string]bool)
	termCounts := make(map[string]int)
	refs := make([]ReverseIndexRef, 0, len(cardRefs))
	for _, ref := range cardRefs {
		key := reverseIndexRefKey(ref.TermType, ref.Term, ref.SceneID, ref.SourceField, ref.RawValue)
		if seenRef[key] {
			continue
		}
		seenRef[key] = true
		termCounts[reverseIndexTermKey(ref.TermType, ref.Term)]++
		refs = append(refs, ref)
	}

	terms := make([]ReverseIndexTerm, 0, len(termCounts))
	for key, count := range termCounts {
		termType, term := splitReverseIndexTermKey(key)
		terms = append(terms, ReverseIndexTerm{TermType: termType, Term: term, OccurrenceCount: count})
	}
	sort.SliceStable(terms, func(i, j int) bool {
		if terms[i].TermType != terms[j].TermType {
			return terms[i].TermType < terms[j].TermType
		}
		return terms[i].Term < terms[j].Term
	})
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].TermType != refs[j].TermType {
			return refs[i].TermType < refs[j].TermType
		}
		if refs[i].Term != refs[j].Term {
			return refs[i].Term < refs[j].Term
		}
		if refs[i].ChapterID != refs[j].ChapterID {
			return refs[i].ChapterID < refs[j].ChapterID
		}
		if refs[i].SceneID != refs[j].SceneID {
			return refs[i].SceneID < refs[j].SceneID
		}
		if refs[i].SourceField != refs[j].SourceField {
			return refs[i].SourceField < refs[j].SourceField
		}
		return refs[i].RawValue < refs[j].RawValue
	})
	return refs, terms
}

func reverseIndexRefsFromCards(cards []reverseIndexSourceCard) []ReverseIndexRef {
	if len(cards) == 0 {
		return nil
	}
	workerLimit := reverseIndexWorkerLimit(len(cards))
	if workerLimit == 1 {
		refs := make([]ReverseIndexRef, 0, len(cards))
		for _, card := range cards {
			refs = append(refs, reverseIndexRefsForCard(card)...)
		}
		return refs
	}

	jobs := make(chan reverseIndexSourceCard)
	results := make(chan []ReverseIndexRef, len(cards))

	var wg sync.WaitGroup
	for i := 0; i < workerLimit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for card := range jobs {
				results <- reverseIndexRefsForCard(card)
			}
		}()
	}

	go func() {
		for _, card := range cards {
			jobs <- card
		}
		close(jobs)
	}()

	wg.Wait()
	close(results)

	refs := make([]ReverseIndexRef, 0, len(cards))
	for batch := range results {
		refs = append(refs, batch...)
	}
	return refs
}

func reverseIndexRefsForCard(card reverseIndexSourceCard) []ReverseIndexRef {
	valuesByField := sceneCardLiteralValues(card.RawJSON)
	refs := make([]ReverseIndexRef, 0, len(reverseTermSources))
	for _, source := range reverseTermSources {
		for _, value := range valuesByField[source.field] {
			refs = append(refs, ReverseIndexRef{
				TermType:    source.termType,
				Term:        value,
				SceneID:     card.SceneID,
				ChapterID:   card.ChapterID,
				SourceField: source.field,
				Weight:      1,
				RawValue:    value,
			})
		}
	}
	return refs
}

func reverseIndexWorkerLimit(cardCount int) int {
	if cardCount <= 1 {
		return 1
	}
	limit := runtime.GOMAXPROCS(0)
	if limit <= 0 {
		limit = 1
	}
	if limit > cardCount {
		limit = cardCount
	}
	return limit
}

func sceneCardLiteralValues(raw string) map[string][]string {
	out := make(map[string][]string, len(reverseTermSources))
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return out
	}
	for _, source := range reverseTermSources {
		if data, ok := obj[source.field]; ok {
			out[source.field] = literalStringValues(data)
		}
	}
	return out
}

func literalStringValues(data json.RawMessage) []string {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		if text == "" {
			return nil
		}
		return []string{text}
	}

	var rawItems []json.RawMessage
	if err := json.Unmarshal(data, &rawItems); err != nil {
		return nil
	}
	out := make([]string, 0, len(rawItems))
	for _, item := range rawItems {
		var value string
		if err := json.Unmarshal(item, &value); err != nil || value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func reverseIndexRefKey(termType, term, sceneID, sourceField, rawValue string) string {
	return termType + "\x00" + term + "\x00" + sceneID + "\x00" + sourceField + "\x00" + rawValue
}

func reverseIndexTermKey(termType, term string) string {
	return termType + "\x00" + term
}

func splitReverseIndexTermKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == 0 {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

// ReverseIndexRefsForChapter returns reverse-index refs for one chapter and a
// set of term types, ordered by scene position and then literal value.
func (s *Store) ReverseIndexRefsForChapter(chapterID string, termTypes []string) ([]ReverseIndexRef, error) {
	termTypes = cleanStringList(termTypes)
	if len(termTypes) == 0 {
		return nil, nil
	}
	query := `SELECT r.term_type, r.term, r.scene_id, r.chapter_id, r.source_field, r.weight, r.raw_value
		 FROM reverse_index_refs r
		 JOIN scenes sn ON sn.id = r.scene_id
		 JOIN chapters c ON c.id = sn.chapter_id
		 WHERE r.chapter_id = ? AND r.term_type IN (` + placeholders(len(termTypes)) + `)
		 ORDER BY c.ordinal, sn.ordinal, r.term_type, r.term, r.source_field, r.raw_value`
	args := []any{chapterID}
	args = append(args, stringsToAny(termTypes)...)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("reverse index refs for chapter %s: %w", chapterID, err)
	}
	defer rows.Close()

	var out []ReverseIndexRef
	for rows.Next() {
		var ref ReverseIndexRef
		if err := rows.Scan(&ref.TermType, &ref.Term, &ref.SceneID, &ref.ChapterID, &ref.SourceField, &ref.Weight, &ref.RawValue); err != nil {
			return nil, fmt.Errorf("reverse index refs for chapter %s: %w", chapterID, err)
		}
		out = append(out, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reverse index refs for chapter %s: %w", chapterID, err)
	}
	return out, nil
}

// ReverseIndexTerms returns literal reverse-index terms for a type and optional prefix.
func (s *Store) ReverseIndexTerms(termType, prefix string, limit int) ([]ReverseIndexTerm, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT term_type, term, occurrence_count
		 FROM reverse_index_terms
         WHERE term_type = ? AND (? = '' OR substr(term, 1, length(?)) = ?)
		 ORDER BY term_type, term
		 LIMIT ?`,
		termType, prefix, prefix, prefix, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("reverse index terms: %w", err)
	}
	defer rows.Close()

	var out []ReverseIndexTerm
	for rows.Next() {
		var term ReverseIndexTerm
		if err := rows.Scan(&term.TermType, &term.Term, &term.OccurrenceCount); err != nil {
			return nil, fmt.Errorf("reverse index terms: %w", err)
		}
		out = append(out, term)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reverse index terms: %w", err)
	}
	return out, nil
}

// ReverseIndexRefs returns supporting scene-card refs for a literal term.
func (s *Store) ReverseIndexRefs(termType, term string) ([]ReverseIndexRef, error) {
	rows, err := s.db.Query(
		`SELECT r.term_type, r.term, r.scene_id, r.chapter_id, r.source_field, r.weight, r.raw_value
		 FROM reverse_index_refs r
		 JOIN scenes sn ON sn.id = r.scene_id
		 JOIN chapters c ON c.id = sn.chapter_id
		 WHERE r.term_type = ? AND r.term = ?
		 ORDER BY c.ordinal, sn.ordinal, r.source_field, r.raw_value`,
		termType, term,
	)
	if err != nil {
		return nil, fmt.Errorf("reverse index refs: %w", err)
	}
	defer rows.Close()

	var out []ReverseIndexRef
	for rows.Next() {
		var ref ReverseIndexRef
		if err := rows.Scan(&ref.TermType, &ref.Term, &ref.SceneID, &ref.ChapterID, &ref.SourceField, &ref.Weight, &ref.RawValue); err != nil {
			return nil, fmt.Errorf("reverse index refs: %w", err)
		}
		out = append(out, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reverse index refs: %w", err)
	}
	return out, nil
}

// InspectReverseIndex returns either exact term refs or prefix term matches.
func (s *Store) InspectReverseIndex(termType, query string, limit int) ([]ReverseIndexTerm, []ReverseIndexRef, error) {
	refs, err := s.ReverseIndexRefs(termType, query)
	if err != nil {
		return nil, nil, err
	}
	if len(refs) > 0 {
		return []ReverseIndexTerm{{TermType: termType, Term: query, OccurrenceCount: len(refs)}}, refs, nil
	}
	terms, err := s.ReverseIndexTerms(termType, query, limit)
	if err != nil {
		return nil, nil, err
	}
	return terms, nil, nil
}
