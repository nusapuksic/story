package store_test

import (
	"testing"

	"github.com/nusapuksic/story/internal/store"
)

func TestReplaceEntityProjectionForChapterReplacesSingleChapterOnly(t *testing.T) {
	s := openTestStore(t)

	insertChapter(t, s, "ch-0001", 1, "Chapter One")
	insertChapter(t, s, "ch-0002", 2, "Chapter Two")
	insertParagraph(t, s, "p-001", "ch-0001", 1)
	insertParagraph(t, s, "p-002", "ch-0002", 1)
	if err := s.InsertScene(store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-001",
		ParagraphEnd:   "p-001",
		Ordinal:        1,
		BoundarySource: "explicit",
		Status:         "generated",
	}); err != nil {
		t.Fatalf("InsertScene ch-0001: %v", err)
	}
	if err := s.InsertScene(store.SceneRow{
		ID:             "sc-002",
		ChapterID:      "ch-0002",
		ParagraphStart: "p-002",
		ParagraphEnd:   "p-002",
		Ordinal:        1,
		BoundarySource: "explicit",
		Status:         "generated",
	}); err != nil {
		t.Fatalf("InsertScene ch-0002: %v", err)
	}

	if err := s.ReplaceEntityProjectionForChapter(
		"ch-0001",
		[]store.EntityRow{{
			ID:              "ent-001",
			ChapterID:       "ch-0001",
			Type:            "character",
			CanonicalName:   "Mara",
			Aliases:         []string{"Maraa"},
			Evidence:        []string{"sc-001"},
			GenerationRun:   "run-1",
			GenerationModel: "test-model",
			PromptVersion:   "entity-v1",
			Status:          "generated",
			RawJSON:         `{"id":"ent-001"}`,
		}},
		[]store.OccurrenceRow{{
			EntityID:        "ent-001",
			ChapterID:       "ch-0001",
			SceneID:         "sc-001",
			SurfaceTexts:    []string{"Mara"},
			SourceFields:    []string{"entities"},
			Confidence:      0.9,
			GenerationRun:   "run-1",
			GenerationModel: "test-model",
			PromptVersion:   "entity-v1",
			Status:          "generated",
			RawJSON:         `{"entity_id":"ent-001","scene_id":"sc-001"}`,
		}},
		1, 1, "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("ReplaceEntityProjectionForChapter ch-0001: %v", err)
	}
	if err := s.ReplaceEntityProjectionForChapter(
		"ch-0002",
		[]store.EntityRow{{
			ID:              "ent-002",
			ChapterID:       "ch-0002",
			Type:            "character",
			CanonicalName:   "Elias",
			Aliases:         nil,
			Evidence:        []string{"sc-002"},
			GenerationRun:   "run-2",
			GenerationModel: "test-model",
			PromptVersion:   "entity-v1",
			Status:          "generated",
			RawJSON:         `{"id":"ent-002"}`,
		}},
		[]store.OccurrenceRow{{
			EntityID:        "ent-002",
			ChapterID:       "ch-0002",
			SceneID:         "sc-002",
			SurfaceTexts:    []string{"Elias"},
			SourceFields:    []string{"participants"},
			Confidence:      0.8,
			GenerationRun:   "run-2",
			GenerationModel: "test-model",
			PromptVersion:   "entity-v1",
			Status:          "generated",
			RawJSON:         `{"entity_id":"ent-002","scene_id":"sc-002"}`,
		}},
		1, 1, "2026-01-02T00:00:00Z",
	); err != nil {
		t.Fatalf("ReplaceEntityProjectionForChapter ch-0002: %v", err)
	}

	maraMatches, err := s.SearchEntities("Maraa", "", 10)
	if err != nil {
		t.Fatalf("SearchEntities alias before replacement: %v", err)
	}
	if len(maraMatches) != 1 || maraMatches[0].ID != "ent-001" {
		t.Fatalf("SearchEntities alias = %#v, want ent-001", maraMatches)
	}

	if err := s.ReplaceEntityProjectionForChapter("ch-0001", nil, nil, 0, 0, "2026-01-03T00:00:00Z"); err != nil {
		t.Fatalf("ReplaceEntityProjectionForChapter ch-0001 empty: %v", err)
	}

	entities, occurrences, err := s.EntityCounts()
	if err != nil {
		t.Fatalf("EntityCounts: %v", err)
	}
	if entities != 1 || occurrences != 1 {
		t.Fatalf("EntityCounts = (%d, %d), want (1, 1)", entities, occurrences)
	}

	ch1Entities, err := s.EntityRowsForChapter("ch-0001")
	if err != nil {
		t.Fatalf("EntityRowsForChapter ch-0001: %v", err)
	}
	if len(ch1Entities) != 0 {
		t.Fatalf("ch-0001 entities = %d, want 0", len(ch1Entities))
	}
	ch2Entities, err := s.EntityRowsForChapter("ch-0002")
	if err != nil {
		t.Fatalf("EntityRowsForChapter ch-0002: %v", err)
	}
	if len(ch2Entities) != 1 || ch2Entities[0].ID != "ent-002" {
		t.Fatalf("ch-0002 entities = %#v, want only ent-002", ch2Entities)
	}
	maraMatches, err = s.SearchEntities("Mara", "", 10)
	if err != nil {
		t.Fatalf("SearchEntities Mara after replacement: %v", err)
	}
	if len(maraMatches) != 0 {
		t.Fatalf("SearchEntities Mara after replacement = %#v, want no stale ent-001", maraMatches)
	}
	eliasMatches, err := s.SearchEntities("Elias", "ch-0002", 10)
	if err != nil {
		t.Fatalf("SearchEntities Elias after replacement: %v", err)
	}
	if len(eliasMatches) != 1 || eliasMatches[0].ID != "ent-002" {
		t.Fatalf("SearchEntities Elias after replacement = %#v, want ent-002", eliasMatches)
	}

	ch1Committed, err := s.IsEntitySnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsEntitySnapshotCommitted ch-0001: %v", err)
	}
	if !ch1Committed {
		t.Fatal("expected ch-0001 entity snapshot to be committed")
	}
	ch2Committed, err := s.IsEntitySnapshotCommitted("ch-0002")
	if err != nil {
		t.Fatalf("IsEntitySnapshotCommitted ch-0002: %v", err)
	}
	if !ch2Committed {
		t.Fatal("expected ch-0002 entity snapshot to be committed")
	}
}
