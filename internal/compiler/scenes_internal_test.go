package compiler

import (
	"context"
	"testing"

	"github.com/nusapuksic/story/internal/store"
)

func TestParagraphCountModeSplitsIntoDefaultEvenSceneCount(t *testing.T) {
	paragraphs := makeSceneTestParagraphs(25)
	scenes, err := detectScenes(context.Background(), nil, store.ChapterRow{ID: "ch-0001"}, paragraphs, nil, nil, nil, "", sceneDetectConfig{Mode: sceneDetectionParagraphCount}, nil)
	if err != nil {
		t.Fatalf("detectScenes: %v", err)
	}

	assertSceneParagraphLengths(t, scenes, paragraphs, []int{7, 6, 6, 6})
	for i := 0; i < len(scenes)-1; i++ {
		if scenes[i].BoundarySource != boundarySourceParagraphCount {
			t.Fatalf("scene %d boundary source = %q, want %q", i, scenes[i].BoundarySource, boundarySourceParagraphCount)
		}
	}
	if scenes[len(scenes)-1].BoundarySource != boundarySourceChapterEnd {
		t.Fatalf("last scene boundary source = %q, want %q", scenes[len(scenes)-1].BoundarySource, boundarySourceChapterEnd)
	}
}

func TestSceneMaxParagraphsAvoidsDanglingOneParagraphTail(t *testing.T) {
	paragraphs := makeSceneTestParagraphs(25)
	scenes, err := detectScenes(context.Background(), nil, store.ChapterRow{ID: "ch-0001"}, paragraphs, nil, nil, nil, "", sceneDetectConfig{Mode: sceneDetectionHybrid, MaxParagraphs: 24}, nil)
	if err != nil {
		t.Fatalf("detectScenes: %v", err)
	}

	assertSceneParagraphLengths(t, scenes, paragraphs, []int{13, 12})
}

func TestTargetSceneCountRespectsExplicitBreaks(t *testing.T) {
	paragraphs := makeSceneTestParagraphs(8)
	scenes, err := detectScenes(context.Background(), nil, store.ChapterRow{ID: "ch-0001"}, paragraphs, nil, []int{5}, nil, "", sceneDetectConfig{Mode: sceneDetectionHybrid, TargetSceneCount: 4}, nil)
	if err != nil {
		t.Fatalf("detectScenes: %v", err)
	}

	assertSceneParagraphLengths(t, scenes, paragraphs, []int{2, 2, 2, 2})
	if scenes[1].BoundarySource != boundarySourceExplicit {
		t.Fatalf("explicit scene boundary source = %q, want %q", scenes[1].BoundarySource, boundarySourceExplicit)
	}
}

func TestTargetSceneCountDoesNotCreateOneParagraphFallbackScenes(t *testing.T) {
	paragraphs := makeSceneTestParagraphs(7)
	scenes, err := detectScenes(context.Background(), nil, store.ChapterRow{ID: "ch-0001"}, paragraphs, nil, nil, nil, "", sceneDetectConfig{Mode: sceneDetectionHybrid, TargetSceneCount: 4}, nil)
	if err != nil {
		t.Fatalf("detectScenes: %v", err)
	}

	assertSceneParagraphLengths(t, scenes, paragraphs, []int{3, 2, 2})
}

func makeSceneTestParagraphs(count int) []store.ParagraphRow {
	paragraphs := make([]store.ParagraphRow, count)
	for i := range paragraphs {
		paragraphs[i] = store.ParagraphRow{
			ID:        "p-" + zeroPaddedSceneTestNumber(i+1),
			ChapterID: "ch-0001",
			Ordinal:   i + 1,
			Text:      "Paragraph.",
		}
	}
	return paragraphs
}

func zeroPaddedSceneTestNumber(value int) string {
	if value < 10 {
		return "00" + string(rune('0'+value))
	}
	return "0" + string(rune('0'+value/10)) + string(rune('0'+value%10))
}

func assertSceneParagraphLengths(t *testing.T, scenes []SceneRecord, paragraphs []store.ParagraphRow, want []int) {
	t.Helper()
	if len(scenes) != len(want) {
		t.Fatalf("scene count = %d, want %d: %#v", len(scenes), len(want), scenes)
	}
	ordByID := make(map[string]int, len(paragraphs))
	for _, paragraph := range paragraphs {
		ordByID[paragraph.ID] = paragraph.Ordinal
	}
	for i, scene := range scenes {
		start, ok := ordByID[scene.ParagraphStart]
		if !ok {
			t.Fatalf("scene %d starts at unknown paragraph %q", i, scene.ParagraphStart)
		}
		end, ok := ordByID[scene.ParagraphEnd]
		if !ok {
			t.Fatalf("scene %d ends at unknown paragraph %q", i, scene.ParagraphEnd)
		}
		got := end - start + 1
		if got != want[i] {
			t.Fatalf("scene %d length = %d, want %d (%s..%s)", i, got, want[i], scene.ParagraphStart, scene.ParagraphEnd)
		}
	}
	if err := ValidateScenePartition(paragraphs, scenes); err != nil {
		t.Fatalf("ValidateScenePartition: %v", err)
	}
}
