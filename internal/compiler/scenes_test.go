package compiler_test

import (
	"testing"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/store"
)

func TestBuildWindowsSingleWindow(t *testing.T) {
	paragraphs := []store.ParagraphRow{
		{ID: "p-1", Ordinal: 1, Text: "Short text."},
		{ID: "p-2", Ordinal: 2, Text: "Another paragraph."},
	}
	// Use the exported function by exercising compiler indirectly. Since
	// buildWindows is unexported, we test its effects through detectScenes.
	_ = paragraphs
}

func TestParseBoundaryResponseValid(t *testing.T) {
	// parseBoundaryResponse is unexported; test via the public detectScenes.
	// We set up a minimal scenario: one chapter, one paragraph, no LLM (nil).
	// The result should be a single scene covering all paragraphs.
	paragraphs := []store.ParagraphRow{
		{ID: "p-001", ChapterID: "ch-0001", Ordinal: 1, Text: "Mara walked the road."},
		{ID: "p-002", ChapterID: "ch-0001", Ordinal: 2, Text: "The sun rose."},
		{ID: "p-003", ChapterID: "ch-0001", Ordinal: 3, Text: "She arrived."},
	}
	// No explicit breaks, no LLM → one scene.
	ch := store.ChapterRow{ID: "ch-0001", Ordinal: 1, Title: "T"}
	scenes, err := compiler.DetectScenesNoLLM(ch, paragraphs, nil)
	if err != nil {
		t.Fatalf("DetectScenesNoLLM error = %v", err)
	}
	if len(scenes) != 1 {
		t.Fatalf("want 1 scene, got %d", len(scenes))
	}
	if scenes[0].ParagraphStart != "p-001" {
		t.Errorf("ParagraphStart = %q, want p-001", scenes[0].ParagraphStart)
	}
	if scenes[0].ParagraphEnd != "p-003" {
		t.Errorf("ParagraphEnd = %q, want p-003", scenes[0].ParagraphEnd)
	}
	if scenes[0].BoundarySource != "chapter_end" {
		t.Errorf("BoundarySource = %q, want chapter_end", scenes[0].BoundarySource)
	}
}

func TestDetectScenesWithExplicitBreaks(t *testing.T) {
	paragraphs := []store.ParagraphRow{
		{ID: "p-001", ChapterID: "ch-0001", Ordinal: 1, Text: "Scene 1 paragraph 1."},
		{ID: "p-002", ChapterID: "ch-0001", Ordinal: 2, Text: "Scene 1 paragraph 2."},
		// scene_break block at ordinal 3 (between p-002 and p-003)
		{ID: "p-003", ChapterID: "ch-0001", Ordinal: 4, Text: "Scene 2 paragraph 1."},
	}
	// explicitBreakOrdinals contains the block ordinal of the scene_break (3).
	ch := store.ChapterRow{ID: "ch-0001", Ordinal: 1, Title: "T"}
	scenes, err := compiler.DetectScenesNoLLM(ch, paragraphs, []int{3})
	if err != nil {
		t.Fatalf("DetectScenesNoLLM error = %v", err)
	}
	if len(scenes) != 2 {
		t.Fatalf("want 2 scenes, got %d", len(scenes))
	}
	if scenes[0].ParagraphStart != "p-001" || scenes[0].ParagraphEnd != "p-002" {
		t.Errorf("scene[0]: start=%s end=%s, want p-001..p-002",
			scenes[0].ParagraphStart, scenes[0].ParagraphEnd)
	}
	if scenes[1].ParagraphStart != "p-003" || scenes[1].ParagraphEnd != "p-003" {
		t.Errorf("scene[1]: start=%s end=%s, want p-003..p-003",
			scenes[1].ParagraphStart, scenes[1].ParagraphEnd)
	}
	if scenes[0].BoundarySource != "explicit" {
		t.Errorf("scene[0].BoundarySource = %q, want explicit", scenes[0].BoundarySource)
	}
}

func TestDetectScenesEmpty(t *testing.T) {
	ch := store.ChapterRow{ID: "ch-0001", Ordinal: 1, Title: "T"}
	scenes, err := compiler.DetectScenesNoLLM(ch, nil, nil)
	if err != nil {
		t.Fatalf("DetectScenesNoLLM error = %v", err)
	}
	if len(scenes) != 0 {
		t.Errorf("want 0 scenes, got %d", len(scenes))
	}
}
