package store_test

import (
	"reflect"
	"testing"

	"github.com/nusapuksic/story/internal/store"
)

func TestRebuildReverseIndexUsesLiteralSceneCardValues(t *testing.T) {
	s := openTestStore(t)
	insertChapter(t, s, `ch-0001`, 1, `Chapter One`)
	insertParagraph(t, s, `p-001`, `ch-0001`, 1)

	if err := s.InsertScene(store.SceneRow{
		ID: `sc-001`, ChapterID: `ch-0001`,
		ParagraphStart: `p-001`, ParagraphEnd: `p-001`,
		Ordinal: 1, BoundarySource: `explicit`, Status: `generated`,
	}); err != nil {
		t.Fatalf(`InsertScene: %v`, err)
	}

	rawJSON := `{"title":"Mara hides the letter","summary":"She hides it.","themes":["Memory","memory"],"entities":["Mara"],"participants":[" Mara ","Mara","Mara"],"pov":["Mara"],"locations":["Lake"],"unresolved":["Who wrote the warning?"],"evidence":["p-001"]}`
	if err := s.InsertSceneCard(store.SceneCardRow{
		SceneID:         `sc-001`,
		Title:           `Mara hides the letter`,
		Summary:         `She hides it.`,
		Evidence:        []string{`p-001`},
		GenerationRun:   `run-001`,
		GenerationModel: `test-model`,
		PromptVersion:   `scene-extraction-v1`,
		Status:          `generated`,
		RawJSON:         rawJSON,
	}); err != nil {
		t.Fatalf(`InsertSceneCard: %v`, err)
	}

	if err := s.RebuildReverseIndex(); err != nil {
		t.Fatalf(`RebuildReverseIndex: %v`, err)
	}

	memoryRefs := requireReverseIndexRefs(t, s, store.ReverseTermTheme, `Memory`, 1)
	if memoryRefs[0].SourceField != `themes` {
		t.Fatalf(`Memory source field = %q, want themes`, memoryRefs[0].SourceField)
	}
	requireReverseIndexRefs(t, s, store.ReverseTermTheme, `memory`, 1)
	requireReverseIndexRefs(t, s, store.ReverseTermEntity, `Mara`, 1)
	requireReverseIndexRefs(t, s, store.ReverseTermParticipant, `Mara`, 1)
	spaced := requireReverseIndexRefs(t, s, store.ReverseTermParticipant, ` Mara `, 1)
	if spaced[0].RawValue != ` Mara ` {
		t.Fatalf(`RawValue = %q, want literal spaced participant`, spaced[0].RawValue)
	}
	requireReverseIndexRefs(t, s, store.ReverseTermUnresolved, `Who wrote the warning?`, 1)

	terms, err := s.ReverseIndexTerms(store.ReverseTermTheme, `Mem`, 10)
	if err != nil {
		t.Fatalf(`ReverseIndexTerms: %v`, err)
	}
	if len(terms) != 1 || terms[0].Term != `Memory` {
		t.Fatalf(`prefix terms = %#v, want only Memory`, terms)
	}
}

func TestDeleteScenesForChapterClearsReverseIndexRefs(t *testing.T) {
	s := openTestStore(t)
	insertChapter(t, s, `ch-0001`, 1, `Chapter One`)
	insertParagraph(t, s, `p-001`, `ch-0001`, 1)

	if err := s.InsertScene(store.SceneRow{
		ID: `sc-001`, ChapterID: `ch-0001`,
		ParagraphStart: `p-001`, ParagraphEnd: `p-001`,
		Ordinal: 1, BoundarySource: `explicit`, Status: `generated`,
	}); err != nil {
		t.Fatalf(`InsertScene: %v`, err)
	}

	if err := s.InsertSceneCard(store.SceneCardRow{
		SceneID:         `sc-001`,
		Title:           `Mara hides the letter`,
		Summary:         `She hides it.`,
		Evidence:        []string{`p-001`},
		GenerationRun:   `run-001`,
		GenerationModel: `test-model`,
		PromptVersion:   `scene-extraction-v1`,
		Status:          `generated`,
		RawJSON:         `{"title":"Mara hides the letter","summary":"She hides it.","themes":["Memory"],"evidence":["p-001"]}`,
	}); err != nil {
		t.Fatalf(`InsertSceneCard: %v`, err)
	}
	if err := s.RebuildReverseIndex(); err != nil {
		t.Fatalf(`RebuildReverseIndex: %v`, err)
	}
	requireReverseIndexRefs(t, s, store.ReverseTermTheme, `Memory`, 1)

	if err := s.DeleteScenesForChapter(`ch-0001`); err != nil {
		t.Fatalf(`DeleteScenesForChapter: %v`, err)
	}

	refs, err := s.ReverseIndexRefs(store.ReverseTermTheme, `Memory`)
	if err != nil {
		t.Fatalf(`ReverseIndexRefs: %v`, err)
	}
	if len(refs) != 0 {
		t.Fatalf(`reverse index refs remain after scene delete: %#v`, refs)
	}
	terms, err := s.ReverseIndexTerms(store.ReverseTermTheme, ``, 10)
	if err != nil {
		t.Fatalf(`ReverseIndexTerms: %v`, err)
	}
	if len(terms) != 0 {
		t.Fatalf(`reverse index terms remain after scene delete: %#v`, terms)
	}
}

func TestRebuildReverseIndexStableAcrossRepeatedRuns(t *testing.T) {
	s := openTestStore(t)
	insertChapter(t, s, `ch-0001`, 1, `Chapter One`)
	insertParagraph(t, s, `p-001`, `ch-0001`, 1)
	insertParagraph(t, s, `p-002`, `ch-0001`, 2)

	if err := s.InsertScene(store.SceneRow{
		ID: `sc-001`, ChapterID: `ch-0001`,
		ParagraphStart: `p-001`, ParagraphEnd: `p-001`,
		Ordinal: 1, BoundarySource: `explicit`, Status: `generated`,
	}); err != nil {
		t.Fatalf(`InsertScene sc-001: %v`, err)
	}
	if err := s.InsertScene(store.SceneRow{
		ID: `sc-002`, ChapterID: `ch-0001`,
		ParagraphStart: `p-002`, ParagraphEnd: `p-002`,
		Ordinal: 2, BoundarySource: `explicit`, Status: `generated`,
	}); err != nil {
		t.Fatalf(`InsertScene sc-002: %v`, err)
	}

	for _, card := range []store.SceneCardRow{
		{
			SceneID:         `sc-001`,
			Title:           `Card 1`,
			Summary:         `Summary 1`,
			Evidence:        []string{`p-001`},
			GenerationRun:   `run-001`,
			GenerationModel: `test-model`,
			PromptVersion:   `scene-extraction-v1`,
			Status:          `generated`,
			RawJSON:         `{"themes":["Memory","Memory"],"entities":["Mara"],"participants":["Mara"],"evidence":["p-001"]}`,
		},
		{
			SceneID:         `sc-002`,
			Title:           `Card 2`,
			Summary:         `Summary 2`,
			Evidence:        []string{`p-002`},
			GenerationRun:   `run-001`,
			GenerationModel: `test-model`,
			PromptVersion:   `scene-extraction-v1`,
			Status:          `generated`,
			RawJSON:         `{"themes":["Memory"],"entities":["Mara"],"participants":["Mara"],"evidence":["p-002"]}`,
		},
	} {
		if err := s.InsertSceneCard(card); err != nil {
			t.Fatalf(`InsertSceneCard %s: %v`, card.SceneID, err)
		}
	}

	if err := s.RebuildReverseIndex(); err != nil {
		t.Fatalf(`first RebuildReverseIndex: %v`, err)
	}
	firstTerms, err := s.ReverseIndexTerms(store.ReverseTermTheme, ``, 20)
	if err != nil {
		t.Fatalf(`ReverseIndexTerms first: %v`, err)
	}
	firstRefs, err := s.ReverseIndexRefs(store.ReverseTermTheme, `Memory`)
	if err != nil {
		t.Fatalf(`ReverseIndexRefs first: %v`, err)
	}

	if err := s.RebuildReverseIndex(); err != nil {
		t.Fatalf(`second RebuildReverseIndex: %v`, err)
	}
	secondTerms, err := s.ReverseIndexTerms(store.ReverseTermTheme, ``, 20)
	if err != nil {
		t.Fatalf(`ReverseIndexTerms second: %v`, err)
	}
	secondRefs, err := s.ReverseIndexRefs(store.ReverseTermTheme, `Memory`)
	if err != nil {
		t.Fatalf(`ReverseIndexRefs second: %v`, err)
	}

	if !reflect.DeepEqual(firstTerms, secondTerms) {
		t.Fatalf("terms changed across rebuilds\nfirst:  %#v\nsecond: %#v", firstTerms, secondTerms)
	}
	if !reflect.DeepEqual(firstRefs, secondRefs) {
		t.Fatalf("refs changed across rebuilds\nfirst:  %#v\nsecond: %#v", firstRefs, secondRefs)
	}
	if len(secondRefs) != 2 {
		t.Fatalf("Memory refs = %d, want 2", len(secondRefs))
	}
}

func requireReverseIndexRefs(t *testing.T, s *store.Store, termType, term string, want int) []store.ReverseIndexRef {
	t.Helper()
	refs, err := s.ReverseIndexRefs(termType, term)
	if err != nil {
		t.Fatalf(`ReverseIndexRefs(%s, %q): %v`, termType, term, err)
	}
	if len(refs) != want {
		t.Fatalf(`ReverseIndexRefs(%s, %q) = %d, want %d: %#v`, termType, term, len(refs), want, refs)
	}
	return refs
}
