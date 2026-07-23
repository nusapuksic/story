package retrieval_test

import (
	"testing"

	"github.com/nusapuksic/story/internal/retrieval"
	"github.com/nusapuksic/story/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
func TestSearchParagraphs(t *testing.T) {
	st := openTestStore(t)

	if err := st.InsertChapterForTest("ch-0001", 1, "The Road"); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	if err := st.InsertParagraphWithTextForTest("p-0001", "ch-0001", 1,
		"Mara walked the long road through the forest."); err != nil {
		t.Fatalf("insert paragraph: %v", err)
	}
	if err := st.InsertParagraphWithTextForTest("p-0002", "ch-0001", 2,
		"The sun rose slowly over the distant hills."); err != nil {
		t.Fatalf("insert paragraph: %v", err)
	}

	result, err := retrieval.Search(st, "Mara road", retrieval.Options{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Paragraphs) == 0 {
		t.Fatal("expected at least one paragraph result for 'Mara road'")
	}
	if result.Paragraphs[0].ID != "p-0001" {
		t.Errorf("expected p-0001 as top result, got %s", result.Paragraphs[0].ID)
	}
}

func TestSearchSceneCards(t *testing.T) {
	st := openTestStore(t)

	if err := st.InsertChapterForTest("ch-0001", 1, "The Road"); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	if err := st.InsertParagraphWithTextForTest("p-0001", "ch-0001", 1, "sample text"); err != nil {
		t.Fatalf("insert paragraph: %v", err)
	}
	if err := st.InsertScene(store.SceneRow{
		ID:             "sc-0001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-0001",
		ParagraphEnd:   "p-0001",
		Ordinal:        1,
		BoundarySource: "explicit",
		Status:         "generated",
	}); err != nil {
		t.Fatalf("insert scene: %v", err)
	}
	if err := st.InsertSceneCard(store.SceneCardRow{
		SceneID:         "sc-0001",
		Title:           "Mara hides the letter",
		Summary:         "Mara receives a letter from Elias and hides it without opening it.",
		Evidence:        []string{"p-0001"},
		GenerationRun:   "compile-test",
		GenerationModel: "test-model",
		PromptVersion:   "v1",
		Status:          "generated",
		RawJSON:         "{}",
	}); err != nil {
		t.Fatalf("insert scene card: %v", err)
	}

	result, err := retrieval.Search(st, "Mara letter", retrieval.Options{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.SceneCards) == 0 {
		t.Fatal("expected at least one scene card result for 'Mara letter'")
	}
	if result.SceneCards[0].SceneID != "sc-0001" {
		t.Errorf("expected sc-0001, got %s", result.SceneCards[0].SceneID)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	st := openTestStore(t)
	result, err := retrieval.Search(st, "", retrieval.Options{})
	if err != nil {
		t.Fatalf("Search with empty query: %v", err)
	}
	if len(result.Paragraphs) != 0 || len(result.SceneCards) != 0 {
		t.Error("expected empty result for empty query")
	}
}

func TestSearchChapterFilter(t *testing.T) {
	st := openTestStore(t)

	for _, ch := range []struct {
		id  string
		ord int
	}{{"ch-0001", 1}, {"ch-0002", 2}} {
		if err := st.InsertChapterForTest(ch.id, ch.ord, ch.id); err != nil {
			t.Fatalf("insert chapter: %v", err)
		}
	}
	if err := st.InsertParagraphWithTextForTest("p-0001", "ch-0001", 1,
		"Mara walked through the forest path."); err != nil {
		t.Fatalf("insert paragraph 1: %v", err)
	}
	if err := st.InsertParagraphWithTextForTest("p-0002", "ch-0002", 1,
		"Mara sat by the river bank quietly."); err != nil {
		t.Fatalf("insert paragraph 2: %v", err)
	}

	// Filter to ch-0001 – only p-0001 should match.
	result, err := retrieval.Search(st, "Mara", retrieval.Options{ChapterID: "ch-0001"})
	if err != nil {
		t.Fatalf("Search with chapter filter: %v", err)
	}
	for _, p := range result.Paragraphs {
		if p.ChapterID != "ch-0001" {
			t.Errorf("got paragraph from chapter %s, want ch-0001", p.ChapterID)
		}
	}
}

func TestSearchSanitizesSpecialChars(t *testing.T) {
	st := openTestStore(t)

	if err := st.InsertChapterForTest("ch-0001", 1, "Chapter"); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	if err := st.InsertParagraphWithTextForTest("p-0001", "ch-0001", 1,
		"Mara opened the door."); err != nil {
		t.Fatalf("insert paragraph: %v", err)
	}

	// Query with FTS special characters should not error.
	_, err := retrieval.Search(st, `"Mara" AND (door OR window)`, retrieval.Options{})
	if err != nil {
		t.Fatalf("Search with special chars: %v", err)
	}
}

func TestSearchSanitizesHyphenatedTerms(t *testing.T) {
	st := openTestStore(t)

	if err := st.InsertChapterForTest("ch-0001", 1, "Chapter"); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	if err := st.InsertParagraphWithTextForTest("p-0001", "ch-0001", 1,
		"The whole book was beautifully maintained."); err != nil {
		t.Fatalf("insert paragraph: %v", err)
	}

	result, err := retrieval.Search(st, "whole-book", retrieval.Options{})
	if err != nil {
		t.Fatalf("Search with hyphenated term: %v", err)
	}
	if len(result.Paragraphs) == 0 || result.Paragraphs[0].ID != "p-0001" {
		t.Fatalf("expected p-0001 for hyphenated query, got %+v", result.Paragraphs)
	}
}
