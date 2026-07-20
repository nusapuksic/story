package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nusapuksic/story/internal/manuscript"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/store"
)

func TestRebuildRestoresScenesAndCardsFromJSONL(t *testing.T) {
	p, p1, p2, p3 := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-01",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p1,
			"ordinal":         1,
			"boundary_source": "explicit",
			"status":          "generated",
		},
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-02",
			"chapter_id":      "ch-0001",
			"paragraph_start": p2,
			"paragraph_end":   p3,
			"ordinal":         2,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  2,
			"committed_at": "2024-01-01T00:00:00Z",
		},
		map[string]any{
			"record_type": "scene_card",
			"scene_id":    "sc-01",
			"title":       "Mara hides the letter",
			"summary":     "She hides the letter.",
			"evidence":    []string{p1},
			"generation": map[string]any{
				"run_id":         "compile-1",
				"model":          "test-model",
				"prompt_version": "scene-extraction-v1",
			},
			"status": "generated",
		},
		map[string]any{
			"record_type": "scene_card",
			"scene_id":    "sc-02",
			"title":       "Sunrise",
			"summary":     "Dawn rises.",
			"evidence":    []string{p2, p3},
			"generation": map[string]any{
				"run_id":         "compile-1",
				"model":          "test-model",
				"prompt_version": "scene-extraction-v1",
			},
			"status": "generated",
		},
	})

	if err := store.Rebuild(p); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	before := readSceneSnapshot(t, p)
	if before.scenes != 2 || before.cards != 2 {
		t.Fatalf("counts before delete = (%d, %d), want (2, 2)", before.scenes, before.cards)
	}
	if len(before.searchIDs) != 1 || before.searchIDs[0] != "sc-01" {
		t.Fatalf("search before delete = %v, want [sc-01]", before.searchIDs)
	}

	if err := os.Remove(p.Path(project.IndexPath)); err != nil {
		t.Fatalf("remove index: %v", err)
	}
	if err := store.Rebuild(p); err != nil {
		t.Fatalf("Rebuild after delete: %v", err)
	}

	after := readSceneSnapshot(t, p)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("snapshot mismatch after rebuild\ngot:  %+v\nwant: %+v", after, before)
	}
}

func TestRebuildScenesJSONLAbsent(t *testing.T) {
	p, _, _, _ := newProjectWithChapter(t)
	if err := os.Remove(p.Path(filepath.Join(project.ModelDir, "scenes.jsonl"))); err != nil {
		t.Fatalf("remove scenes.jsonl: %v", err)
	}
	if err := store.Rebuild(p); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	st := openProjectStore(t, p)
	scenes, cards, err := st.SceneCounts()
	if err != nil {
		t.Fatalf("SceneCounts: %v", err)
	}
	if scenes != 0 || cards != 0 {
		t.Fatalf("counts = (%d, %d), want (0, 0)", scenes, cards)
	}
}

func TestRebuildFailsOnMalformedScenesJSONL(t *testing.T) {
	p, _, _, _ := newProjectWithChapter(t)
	writeScenesLines(t, p, `{"record_type":"scene","id":"sc-01"`, `}`)

	err := store.Rebuild(p)
	if err == nil || !strings.Contains(err.Error(), "malformed json") {
		t.Fatalf("Rebuild error = %v, want malformed json error", err)
	}
}

func TestRebuildFailsOnMissingSceneParagraph(t *testing.T) {
	p, _, _, _ := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-01",
			"chapter_id":      "ch-0001",
			"paragraph_start": "p-missing",
			"paragraph_end":   "p-missing",
			"ordinal":         1,
			"boundary_source": "explicit",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  1,
			"committed_at": "2024-01-01T00:00:00Z",
		},
	})
	err := store.Rebuild(p)
	if err == nil || !strings.Contains(err.Error(), "missing paragraph_start") {
		t.Fatalf("Rebuild error = %v, want missing paragraph_start", err)
	}
}

func TestRebuildFailsOnMissingSceneForCard(t *testing.T) {
	p, p1, _, _ := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		map[string]any{
			"record_type": "scene_card",
			"scene_id":    "sc-missing",
			"title":       "Missing scene",
			"summary":     "This should fail.",
			"evidence":    []string{p1},
			"generation": map[string]any{
				"run_id":         "compile-1",
				"model":          "test-model",
				"prompt_version": "scene-extraction-v1",
			},
			"status": "generated",
		},
	})
	err := store.Rebuild(p)
	if err == nil || !strings.Contains(err.Error(), "references missing scene") {
		t.Fatalf("Rebuild error = %v, want missing scene error", err)
	}
}

func TestRebuildUsesLatestReplacementSnapshot(t *testing.T) {
	p, p1, p2, p3 := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		// First compile run: one scene covering the full chapter.
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-old",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p3,
			"ordinal":         1,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  1,
			"committed_at": "2024-01-01T00:00:00Z",
		},
		map[string]any{
			"record_type": "scene_card",
			"scene_id":    "sc-old",
			"title":       "Old card",
			"summary":     "Old summary",
			"evidence":    []string{p1},
			"generation": map[string]any{
				"run_id":         "compile-old",
				"model":          "test-model",
				"prompt_version": "scene-extraction-v1",
			},
			"status": "generated",
		},
		// Second compile run: new scene replaces old via ordinal==1 reset + new snapshot.
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-new",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p3,
			"ordinal":         1,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  1,
			"committed_at": "2024-01-02T00:00:00Z",
		},
		map[string]any{
			"record_type": "scene_card",
			"scene_id":    "sc-new",
			"title":       "New card",
			"summary":     "New summary",
			"evidence":    []string{p2},
			"generation": map[string]any{
				"run_id":         "compile-new",
				"model":          "test-model",
				"prompt_version": "scene-extraction-v1",
			},
			"status": "generated",
		},
	})

	if err := store.Rebuild(p); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	st := openProjectStore(t, p)
	scenes, err := st.AllScenes()
	if err != nil {
		t.Fatalf("AllScenes: %v", err)
	}
	if len(scenes) != 1 || scenes[0].ID != "sc-new" {
		t.Fatalf("active scenes = %+v, want only sc-new", scenes)
	}
	card, err := st.InspectSceneCard("sc-new")
	if err != nil {
		t.Fatalf("InspectSceneCard(sc-new): %v", err)
	}
	if card.Title != "New card" {
		t.Fatalf("card title = %q, want New card", card.Title)
	}
	if _, err := st.InspectSceneCard("sc-old"); err == nil {
		t.Fatal("expected old scene card to be superseded")
	}
}

func TestRebuildCommittedEmptySnapshotReplacesPrevious(t *testing.T) {
	p, p1, _, p3 := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-old",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p3,
			"ordinal":         1,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  1,
			"committed_at": "2024-01-01T00:00:00Z",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  0,
			"committed_at": "2024-01-02T00:00:00Z",
		},
	})

	if err := store.Rebuild(p); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	st := openProjectStore(t, p)
	scenes, cards, err := st.SceneCounts()
	if err != nil {
		t.Fatalf("SceneCounts: %v", err)
	}
	if scenes != 0 || cards != 0 {
		t.Fatalf("counts = (%d, %d), want (0, 0)", scenes, cards)
	}
	committed, err := st.IsChapterSnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsChapterSnapshotCommitted: %v", err)
	}
	if !committed {
		t.Fatal("chapter should remain committed after empty replacement snapshot")
	}
}

func TestRebuildFailsOnSnapshotSceneCountMismatch(t *testing.T) {
	p, p1, _, p3 := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-one",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p3,
			"ordinal":         1,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  2,
			"committed_at": "2024-01-01T00:00:00Z",
		},
	})

	err := store.Rebuild(p)
	if err == nil || !strings.Contains(err.Error(), "scene_count mismatch") {
		t.Fatalf("Rebuild error = %v, want scene_count mismatch", err)
	}
}

func TestRebuildFailsOnSceneOrdinalGap(t *testing.T) {
	p, p1, p2, p3 := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-1",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p1,
			"ordinal":         1,
			"boundary_source": "explicit",
			"status":          "generated",
		},
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-2",
			"chapter_id":      "ch-0001",
			"paragraph_start": p2,
			"paragraph_end":   p3,
			"ordinal":         3,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  2,
			"committed_at": "2024-01-01T00:00:00Z",
		},
	})

	err := store.Rebuild(p)
	if err == nil || !strings.Contains(err.Error(), "expected 2") {
		t.Fatalf("Rebuild error = %v, want ordinal sequence error", err)
	}
}

func TestRebuildFailsOnSceneOrdinalStartingAtTwo(t *testing.T) {
	p, p1, p2, p3 := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-1",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p1,
			"ordinal":         2,
			"boundary_source": "explicit",
			"status":          "generated",
		},
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-2",
			"chapter_id":      "ch-0001",
			"paragraph_start": p2,
			"paragraph_end":   p3,
			"ordinal":         3,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  2,
			"committed_at": "2024-01-01T00:00:00Z",
		},
	})

	err := store.Rebuild(p)
	if err == nil || !strings.Contains(err.Error(), "expected 1") {
		t.Fatalf("Rebuild error = %v, want ordinal sequence starting at 1 error", err)
	}
}

func TestRebuildCompleteSnapshotSurvivesInterruptedReplacement(t *testing.T) {
	p, p1, p2, p3 := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-old-1",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p1,
			"ordinal":         1,
			"boundary_source": "explicit",
			"status":          "generated",
		},
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-old-2",
			"chapter_id":      "ch-0001",
			"paragraph_start": p2,
			"paragraph_end":   p3,
			"ordinal":         2,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  2,
			"committed_at": "2024-01-01T00:00:00Z",
		},
		// Replacement starts but is interrupted before chapter_snapshot.
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-new-partial",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p2,
			"ordinal":         1,
			"boundary_source": "explicit",
			"status":          "generated",
		},
	})

	if err := store.Rebuild(p); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	st := openProjectStore(t, p)
	allScenes, err := st.AllScenes()
	if err != nil {
		t.Fatalf("AllScenes: %v", err)
	}
	if len(allScenes) != 2 {
		t.Fatalf("want 2 scenes from last committed snapshot, got %d", len(allScenes))
	}
	if allScenes[0].ID != "sc-old-1" || allScenes[1].ID != "sc-old-2" {
		t.Fatalf("active scenes = %+v, want [sc-old-1 sc-old-2]", allScenes)
	}
}

func TestRebuildFailureKeepsExistingIndex(t *testing.T) {
	p, p1, p2, p3 := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-keep",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p3,
			"ordinal":         1,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  1,
			"committed_at": "2024-01-01T00:00:00Z",
		},
	})
	_ = p2 // used via p1..p3 coverage
	if err := store.Rebuild(p); err != nil {
		t.Fatalf("initial Rebuild: %v", err)
	}

	writeScenesLines(t, p, `{"record_type":"scene","id":"sc-bad"`)
	if err := store.Rebuild(p); err == nil {
		t.Fatal("expected rebuild to fail")
	}

	st := openProjectStore(t, p)
	scenes, cards, err := st.SceneCounts()
	if err != nil {
		t.Fatalf("SceneCounts: %v", err)
	}
	if scenes != 1 || cards != 0 {
		t.Fatalf("counts after failed rebuild = (%d, %d), want (1, 0)", scenes, cards)
	}
	if _, err := st.InspectScene("sc-keep"); err != nil {
		t.Fatalf("expected previous index content to remain: %v", err)
	}
}

// TestIncompleteJSONLSnapshotIsDiscarded verifies that scene records without a
// following chapter_snapshot record are not loaded into the index.
func TestIncompleteJSONLSnapshotIsDiscarded(t *testing.T) {
	p, p1, _, p3 := newProjectWithChapter(t)
	// Write scenes for ch-0001 WITHOUT a chapter_snapshot record.
	writeScenesJSONL(t, p, []any{
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-incomplete",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p3,
			"ordinal":         1,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		// No chapter_snapshot: this represents an interrupted compile.
	})
	if err := store.Rebuild(p); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	st := openProjectStore(t, p)
	scenes, cards, err := st.SceneCounts()
	if err != nil {
		t.Fatalf("SceneCounts: %v", err)
	}
	if scenes != 0 || cards != 0 {
		t.Errorf("incomplete snapshot should produce no scenes/cards; got (%d, %d)", scenes, cards)
	}
}

// TestExplicitChapterSnapshotCommitted verifies that scenes followed by a
// chapter_snapshot record are loaded and marked committed in the index.
func TestExplicitChapterSnapshotCommitted(t *testing.T) {
	p, p1, _, p3 := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-full",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p3,
			"ordinal":         1,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  1,
			"committed_at": "2024-06-01T12:00:00Z",
		},
	})
	if err := store.Rebuild(p); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	st := openProjectStore(t, p)
	scenes, _, err := st.SceneCounts()
	if err != nil {
		t.Fatalf("SceneCounts: %v", err)
	}
	if scenes != 1 {
		t.Errorf("expected 1 committed scene, got %d", scenes)
	}
	committed, err := st.IsChapterSnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsChapterSnapshotCommitted: %v", err)
	}
	if !committed {
		t.Error("chapter_scene_snapshots should mark ch-0001 as committed after rebuild")
	}
}

// TestInterruptedJSONLSnapshotDoesNotCorruptSubsequentCompleteRun verifies that
// scenes from an interrupted first run are discarded when a complete second run
// (with chapter_snapshot) follows in the same JSONL file.
func TestInterruptedJSONLSnapshotDoesNotCorruptSubsequentCompleteRun(t *testing.T) {
	p, p1, p2, p3 := newProjectWithChapter(t)
	writeScenesJSONL(t, p, []any{
		// First run (interrupted): only ordinal=1, no chapter_snapshot.
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-partial",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p1,
			"ordinal":         1,
			"boundary_source": "explicit",
			"status":          "generated",
		},
		// Second run (complete): ordinal=1 resets the pending buffer.
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-a",
			"chapter_id":      "ch-0001",
			"paragraph_start": p1,
			"paragraph_end":   p1,
			"ordinal":         1,
			"boundary_source": "explicit",
			"status":          "generated",
		},
		map[string]any{
			"record_type":     "scene",
			"id":              "sc-b",
			"chapter_id":      "ch-0001",
			"paragraph_start": p2,
			"paragraph_end":   p3,
			"ordinal":         2,
			"boundary_source": "chapter_end",
			"status":          "generated",
		},
		map[string]any{
			"record_type":  "chapter_snapshot",
			"chapter_id":   "ch-0001",
			"scene_count":  2,
			"committed_at": "2024-06-02T00:00:00Z",
		},
	})
	if err := store.Rebuild(p); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	st := openProjectStore(t, p)
	allScenes, err := st.AllScenes()
	if err != nil {
		t.Fatalf("AllScenes: %v", err)
	}
	if len(allScenes) != 2 {
		t.Fatalf("want 2 scenes from complete run, got %d", len(allScenes))
	}
	for _, sc := range allScenes {
		if sc.ID == "sc-partial" {
			t.Error("partial scene from interrupted run should not appear in index")
		}
	}
}

func newProjectWithChapter(t *testing.T) (*project.Project, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	p, err := project.Init(dir, project.InitOptions{Title: "Test", Language: "en"})
	if err != nil {
		t.Fatalf("project.Init: %v", err)
	}

	p1 := "p-0001"
	p2 := "p-0002"
	p3 := "p-0003"
	ch := &manuscript.Chapter{
		ID:        "ch-0001",
		Order:     1,
		Title:     "Chapter One",
		File:      "chapters/ch-0001.md",
		SourceKey: "01-chapter-one.md",
		Blocks: []manuscript.Block{
			{Type: manuscript.BlockParagraph, ParagraphID: p1, Text: "Mara receives a letter."},
			{Type: manuscript.BlockParagraph, ParagraphID: p2, Text: "She hides it."},
			{Type: manuscript.BlockParagraph, ParagraphID: p3, Text: "Morning arrives."},
		},
	}
	if err := manuscript.WriteChapter(p.Path(project.ManuscriptDir), ch); err != nil {
		t.Fatalf("WriteChapter: %v", err)
	}
	toc := manuscript.TOC{
		Version: 1,
		Chapters: []manuscript.TOCEntry{
			{ID: ch.ID, Order: ch.Order, Title: ch.Title, File: ch.File, SourceKey: ch.SourceKey},
		},
	}
	if err := manuscript.SaveTOC(p.Path(project.TOCPath), toc); err != nil {
		t.Fatalf("SaveTOC: %v", err)
	}
	return p, p1, p2, p3
}

func writeScenesJSONL(t *testing.T, p *project.Project, records []any) {
	t.Helper()
	var lines []string
	for _, rec := range records {
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("marshal scene record: %v", err)
		}
		lines = append(lines, string(b))
	}
	writeScenesLines(t, p, lines...)
}

func writeScenesLines(t *testing.T, p *project.Project, lines ...string) {
	t.Helper()
	content := ""
	for _, ln := range lines {
		content += ln + "\n"
	}
	if err := os.WriteFile(p.Path(filepath.Join(project.ModelDir, "scenes.jsonl")), []byte(content), 0o644); err != nil {
		t.Fatalf("write scenes.jsonl: %v", err)
	}
}

func openProjectStore(t *testing.T, p *project.Project) *store.Store {
	t.Helper()
	st, err := store.Open(p.Path(project.IndexPath))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

type sceneSnapshot struct {
	scenes    int
	cards     int
	searchIDs []string
	sceneIDs  []string
}

func readSceneSnapshot(t *testing.T, p *project.Project) sceneSnapshot {
	t.Helper()
	st, err := store.Open(p.Path(project.IndexPath))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	scenes, cards, err := st.SceneCounts()
	if err != nil {
		_ = st.Close()
		t.Fatalf("SceneCounts: %v", err)
	}
	allScenes, err := st.AllScenes()
	if err != nil {
		_ = st.Close()
		t.Fatalf("AllScenes: %v", err)
	}
	var sceneIDs []string
	for _, sc := range allScenes {
		sceneIDs = append(sceneIDs, sc.ID)
	}
	found, err := st.SearchSceneCards("hides letter", 10)
	if err != nil {
		_ = st.Close()
		t.Fatalf("SearchSceneCards: %v", err)
	}
	var searchIDs []string
	for _, card := range found {
		searchIDs = append(searchIDs, card.SceneID)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Store.Close: %v", err)
	}
	return sceneSnapshot{
		scenes:    scenes,
		cards:     cards,
		searchIDs: searchIDs,
		sceneIDs:  sceneIDs,
	}
}
