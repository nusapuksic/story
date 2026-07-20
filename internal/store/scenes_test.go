package store_test

import (
	"path/filepath"
	"testing"

	"github.com/nusapuksic/story/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "index.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertAndQueryScene(t *testing.T) {
	s := openTestStore(t)

	// Need a chapter row before we can insert a scene.
	insertChapter(t, s, "ch-0001", 1, "Chapter One")
	insertParagraph(t, s, "p-001", "ch-0001", 1)
	insertParagraph(t, s, "p-002", "ch-0001", 2)

	row := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-001",
		ParagraphEnd:   "p-002",
		Ordinal:        1,
		BoundarySource: "explicit",
		Status:         "generated",
	}
	if err := s.InsertScene(row); err != nil {
		t.Fatalf("InsertScene: %v", err)
	}

	got, err := s.InspectScene("sc-001")
	if err != nil {
		t.Fatalf("InspectScene: %v", err)
	}
	if got.ChapterID != "ch-0001" {
		t.Errorf("ChapterID = %q, want ch-0001", got.ChapterID)
	}
	if got.BoundarySource != "explicit" {
		t.Errorf("BoundarySource = %q, want explicit", got.BoundarySource)
	}
}

func TestInsertAndQuerySceneCard(t *testing.T) {
	s := openTestStore(t)
	insertChapter(t, s, "ch-0001", 1, "Chapter One")
	insertParagraph(t, s, "p-001", "ch-0001", 1)

	sceneRow := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-001",
		ParagraphEnd:   "p-001",
		Ordinal:        1,
		BoundarySource: "chapter_end",
		Status:         "generated",
	}
	if err := s.InsertScene(sceneRow); err != nil {
		t.Fatalf("InsertScene: %v", err)
	}

	card := store.SceneCardRow{
		SceneID:         "sc-001",
		Title:           "Mara hides the letter",
		Summary:         "She hides it.",
		Evidence:        []string{"p-001"},
		GenerationRun:   "compile-001",
		GenerationModel: "test-model",
		PromptVersion:   "scene-extraction-v1",
		Status:          "generated",
		RawJSON:         `{"title":"Mara hides the letter"}`,
	}
	if err := s.InsertSceneCard(card); err != nil {
		t.Fatalf("InsertSceneCard: %v", err)
	}

	got, err := s.InspectSceneCard("sc-001")
	if err != nil {
		t.Fatalf("InspectSceneCard: %v", err)
	}
	if got.Title != "Mara hides the letter" {
		t.Errorf("Title = %q", got.Title)
	}
	if len(got.Evidence) != 1 || got.Evidence[0] != "p-001" {
		t.Errorf("Evidence = %v", got.Evidence)
	}
}

func TestSceneNotFound(t *testing.T) {
	s := openTestStore(t)
	_, err := s.InspectScene("sc-NONEXISTENT")
	if err == nil {
		t.Fatal("expected ErrNotFound")
	}
}

func TestSceneBreakOrdinals(t *testing.T) {
	s := openTestStore(t)
	insertChapter(t, s, "ch-0001", 1, "Chapter One")
	// Insert blocks directly via a helper—blocks only exist when using Rebuild,
	// so we test through SceneBreakOrdinals returning empty when no blocks.
	ordinals, err := s.SceneBreakOrdinals("ch-0001")
	if err != nil {
		t.Fatalf("SceneBreakOrdinals: %v", err)
	}
	if len(ordinals) != 0 {
		t.Errorf("expected 0 scene break ordinals, got %v", ordinals)
	}
}

func TestDeleteScenesForChapter(t *testing.T) {
	s := openTestStore(t)
	insertChapter(t, s, "ch-0001", 1, "Chapter One")
	insertParagraph(t, s, "p-001", "ch-0001", 1)

	if err := s.InsertScene(store.SceneRow{
		ID: "sc-001", ChapterID: "ch-0001",
		ParagraphStart: "p-001", ParagraphEnd: "p-001",
		Ordinal: 1, BoundarySource: "chapter_end", Status: "generated",
	}); err != nil {
		t.Fatal(err)
	}

	scenes, _ := s.ScenesByChapter("ch-0001")
	if len(scenes) != 1 {
		t.Fatalf("expected 1 scene before delete, got %d", len(scenes))
	}

	if err := s.DeleteScenesForChapter("ch-0001"); err != nil {
		t.Fatalf("DeleteScenesForChapter: %v", err)
	}
	scenes, _ = s.ScenesByChapter("ch-0001")
	if len(scenes) != 0 {
		t.Errorf("expected 0 scenes after delete, got %d", len(scenes))
	}
}

func TestSceneCounts(t *testing.T) {
	s := openTestStore(t)
	scenes, cards, err := s.SceneCounts()
	if err != nil {
		t.Fatalf("SceneCounts: %v", err)
	}
	if scenes != 0 || cards != 0 {
		t.Errorf("expected 0,0 got %d,%d", scenes, cards)
	}
}

func TestMarkAndCheckChapterSnapshotCommitted(t *testing.T) {
	s := openTestStore(t)
	insertChapter(t, s, "ch-0001", 1, "Chapter One")

	committed, err := s.IsChapterSnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsChapterSnapshotCommitted (before): %v", err)
	}
	if committed {
		t.Error("chapter should not be committed before MarkChapterSnapshotCommitted")
	}

	if err := s.MarkChapterSnapshotCommitted("ch-0001", "2024-01-01T00:00:00Z"); err != nil {
		t.Fatalf("MarkChapterSnapshotCommitted: %v", err)
	}

	committed, err = s.IsChapterSnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsChapterSnapshotCommitted (after): %v", err)
	}
	if !committed {
		t.Error("chapter should be committed after MarkChapterSnapshotCommitted")
	}

	// Idempotent: calling again should succeed (REPLACE semantics).
	if err := s.MarkChapterSnapshotCommitted("ch-0001", "2024-01-02T00:00:00Z"); err != nil {
		t.Errorf("MarkChapterSnapshotCommitted (idempotent): %v", err)
	}
}

func TestDeleteChapterSnapshot(t *testing.T) {
	s := openTestStore(t)
	insertChapter(t, s, "ch-0001", 1, "Chapter One")

	if err := s.MarkChapterSnapshotCommitted("ch-0001", "2024-01-01T00:00:00Z"); err != nil {
		t.Fatalf("MarkChapterSnapshotCommitted: %v", err)
	}
	if err := s.DeleteChapterSnapshot("ch-0001"); err != nil {
		t.Fatalf("DeleteChapterSnapshot: %v", err)
	}
	committed, err := s.IsChapterSnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsChapterSnapshotCommitted: %v", err)
	}
	if committed {
		t.Error("chapter snapshot should be gone after DeleteChapterSnapshot")
	}
}

func TestDeleteScenesForChapterAlsoClearsSnapshot(t *testing.T) {
	s := openTestStore(t)
	insertChapter(t, s, "ch-0001", 1, "Chapter One")
	insertParagraph(t, s, "p-001", "ch-0001", 1)

	if err := s.InsertScene(store.SceneRow{
		ID: "sc-001", ChapterID: "ch-0001",
		ParagraphStart: "p-001", ParagraphEnd: "p-001",
		Ordinal: 1, BoundarySource: "chapter_end", Status: "generated",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkChapterSnapshotCommitted("ch-0001", "2024-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteScenesForChapter("ch-0001"); err != nil {
		t.Fatalf("DeleteScenesForChapter: %v", err)
	}

	committed, err := s.IsChapterSnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsChapterSnapshotCommitted: %v", err)
	}
	if committed {
		t.Error("DeleteScenesForChapter should also clear the chapter snapshot marker")
	}
}

// helpers

func insertChapter(t *testing.T, s *store.Store, id string, ordinal int, title string) {
	t.Helper()
	// Use the store's internal DB via IndexChapters would rebuild everything.
	// Instead write directly through the exported InsertChapterForTest helper.
	// Since Store doesn't have a public InsertChapter, we use Rebuild path.
	// For simplicity, just use a temp project and Rebuild.
	_ = s
	_ = id
	_ = ordinal
	_ = title
	// We need a way to insert test chapters. Use the exported helper.
	if err := insertChapterHelper(s, id, ordinal, title); err != nil {
		t.Fatalf("insertChapter: %v", err)
	}
}

func insertParagraph(t *testing.T, s *store.Store, id, chapterID string, ordinal int) {
	t.Helper()
	if err := insertParagraphHelper(s, id, chapterID, ordinal); err != nil {
		t.Fatalf("insertParagraph: %v", err)
	}
}

// insertChapterHelper and insertParagraphHelper call exported helpers on Store.
func insertChapterHelper(s *store.Store, id string, ordinal int, title string) error {
	return s.InsertChapterForTest(id, ordinal, title)
}

func insertParagraphHelper(s *store.Store, id, chapterID string, ordinal int) error {
	return s.InsertParagraphForTest(id, chapterID, ordinal)
}
