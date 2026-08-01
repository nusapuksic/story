package store_test

import (
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
