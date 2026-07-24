package compiler_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/manuscript"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/store"
)

// buildTestProject creates a minimal project with one chapter containing two
// paragraphs and an explicit scene break.
func buildTestProject(t *testing.T) (*project.Project, *store.Store) {
	t.Helper()
	dir := t.TempDir()

	// Init project.
	p, err := project.Init(dir, project.InitOptions{Title: "Test Novel", Language: "en"})
	if err != nil {
		t.Fatalf("project.Init: %v", err)
	}

	// Create one chapter with two paragraphs and a scene break.
	ch := &manuscript.Chapter{
		ID:        ids.ChapterID(1),
		Order:     1,
		Title:     "The Road",
		File:      "chapters/ch-0001.md",
		SourceKey: "01-road.md",
		Blocks: []manuscript.Block{
			{Type: manuscript.BlockParagraph, ParagraphID: ids.NewParagraphID(), Text: "Mara walked the road."},
			{Type: manuscript.BlockSceneBreak, Text: "***"},
			{Type: manuscript.BlockParagraph, ParagraphID: ids.NewParagraphID(), Text: "The sun rose over the hills."},
		},
	}
	chapDir := p.Path(project.ChaptersDir)
	if err := manuscript.WriteChapter(p.Path(project.ManuscriptDir), ch); err != nil {
		t.Fatalf("WriteChapter: %v", err)
	}
	_ = chapDir

	// Write TOC.
	toc := manuscript.TOC{
		Version: 1,
		Chapters: []manuscript.TOCEntry{
			{ID: ch.ID, Order: ch.Order, Title: ch.Title, File: ch.File, SourceKey: ch.SourceKey},
		},
	}
	if err := manuscript.SaveTOC(p.Path(project.TOCPath), toc); err != nil {
		t.Fatalf("SaveTOC: %v", err)
	}

	// Rebuild the SQLite index.
	if err := store.Rebuild(p); err != nil {
		t.Fatalf("store.Rebuild: %v", err)
	}

	st, err := store.Open(p.Path(project.IndexPath))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	return p, st
}

func TestCompileScenesExplicitOnly(t *testing.T) {
	p, st := buildTestProject(t)

	opts := compiler.Options{
		Layer: compiler.LayerScenes,
	}
	result, err := compiler.Compile(context.Background(), p, st, opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if result.ScenesBuilt != 2 {
		t.Errorf("ScenesBuilt = %d, want 2 (one scene per explicit break + chapter end)", result.ScenesBuilt)
	}

	scenes, err := st.AllScenes()
	if err != nil {
		t.Fatalf("AllScenes: %v", err)
	}
	if len(scenes) != 2 {
		t.Fatalf("want 2 scenes in store, got %d", len(scenes))
	}

	// Verify JSONL file was written.
	scenesPath := p.Path(filepath.Join(project.ModelDir, "scenes.jsonl"))
	data, err := os.ReadFile(scenesPath)
	if err != nil {
		t.Fatalf("read scenes.jsonl: %v", err)
	}
	if len(data) == 0 {
		t.Error("scenes.jsonl is empty")
	}

	// Verify run directory was created.
	runs, err := os.ReadDir(p.Path(project.RunsDir))
	if err != nil {
		t.Fatalf("read runs dir: %v", err)
	}
	if len(runs) == 0 {
		t.Error("no run directory created")
	}
}

func TestCompileSceneCardsWithFakeProvider(t *testing.T) {
	p, st := buildTestProject(t)

	// First compile scenes.
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}

	// Now compile scene cards with a fake provider.
	cardJSON := `{"title":"Mara walks","summary":"Mara walks the road.","evidence":[]}`
	fake := &fakeProvider{response: cardJSON}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile scene-cards: %v", err)
	}
	if result.CardsBuilt != 2 {
		t.Errorf("CardsBuilt = %d, want 2", result.CardsBuilt)
	}
}

func TestCompileRequiresProviderForSceneCards(t *testing.T) {
	p, st := buildTestProject(t)

	// Compile scenes first.
	compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerScenes}) //nolint

	// Compiling scene-cards without a provider should fail.
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerSceneCards,
	})
	if err == nil {
		t.Fatal("expected error when no provider configured for scene-cards")
	}
}

func TestCompileUnknownLayer(t *testing.T) {
	p, st := buildTestProject(t)
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown layer")
	}
}

func TestCompileChapterFilter(t *testing.T) {
	p, st := buildTestProject(t)

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:     compiler.LayerScenes,
		ChapterID: "ch-0001",
	})
	if err != nil {
		t.Fatalf("Compile with chapter filter: %v", err)
	}
	if result.ScenesBuilt == 0 {
		t.Error("expected at least 1 scene when filtering to ch-0001")
	}
}

func TestCompileForceRecompute(t *testing.T) {
	p, st := buildTestProject(t)

	// Compile twice; second run with --force should recompute.
	compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerScenes}) //nolint
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
		Force: true,
	})
	if err != nil {
		t.Fatalf("force recompile: %v", err)
	}
	if result.ScenesBuilt == 0 {
		t.Error("force recompile should produce scenes")
	}
}

// TestInterruptedSnapshotRecoversOnNextCompile verifies that a chapter whose
// scenes were partially written (no committed chapter_snapshot) is detected and
// reprocessed on the next compile run rather than being silently skipped.
func TestInterruptedSnapshotRecoversOnNextCompile(t *testing.T) {
	p, st := buildTestProject(t)

	// Simulate an interrupted compile: manually insert only the first scene for
	// the chapter without calling MarkChapterSnapshotCommitted.  This represents
	// a run that crashed after inserting some scenes but before committing the
	// snapshot.
	scenes, err := st.AllChapters()
	if err != nil {
		t.Fatalf("AllChapters: %v", err)
	}
	if len(scenes) == 0 {
		t.Fatal("expected at least one chapter")
	}
	ch := scenes[0]
	paragraphs, err := st.ParagraphsByChapter(ch.ID)
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	if len(paragraphs) < 2 {
		t.Fatalf("expected at least 2 paragraphs, got %d", len(paragraphs))
	}

	// Insert only the first scene (partial snapshot – no chapter_snapshot marker).
	if err := st.InsertScene(store.SceneRow{
		ID:             "sc-partial",
		ChapterID:      ch.ID,
		ParagraphStart: paragraphs[0].ID,
		ParagraphEnd:   paragraphs[0].ID,
		Ordinal:        1,
		BoundarySource: "explicit",
		Status:         "generated",
	}); err != nil {
		t.Fatalf("InsertScene (partial): %v", err)
	}
	// Confirm the partial scene exists and no snapshot is committed.
	existing, err := st.ScenesByChapter(ch.ID)
	if err != nil {
		t.Fatalf("ScenesByChapter: %v", err)
	}
	if len(existing) != 1 {
		t.Fatalf("expected 1 partial scene before compile, got %d", len(existing))
	}
	committed, err := st.IsChapterSnapshotCommitted(ch.ID)
	if err != nil {
		t.Fatalf("IsChapterSnapshotCommitted: %v", err)
	}
	if committed {
		t.Fatal("chapter should not be marked committed before test compile")
	}

	// Run compile: it must detect the uncommitted partial scenes, discard them,
	// and produce a complete, committed snapshot.
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("Compile after interrupted snapshot: %v", err)
	}
	if result.ScenesBuilt == 0 {
		t.Error("expected scenes to be rebuilt after interrupted snapshot")
	}

	// After compile, partial scene must be gone and snapshot committed.
	allScenes, err := st.ScenesByChapter(ch.ID)
	if err != nil {
		t.Fatalf("ScenesByChapter after compile: %v", err)
	}
	for _, sc := range allScenes {
		if sc.ID == "sc-partial" {
			t.Error("partial scene sc-partial should have been discarded and replaced")
		}
	}
	committed, err = st.IsChapterSnapshotCommitted(ch.ID)
	if err != nil {
		t.Fatalf("IsChapterSnapshotCommitted after compile: %v", err)
	}
	if !committed {
		t.Error("chapter snapshot should be committed after successful compile")
	}
}

// TestSnapshotCommittedPreventsRecompile verifies that a chapter with a committed
// snapshot is not reprocessed on a subsequent compile without --force.
func TestSnapshotCommittedPreventsRecompile(t *testing.T) {
	p, st := buildTestProject(t)

	first, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("first compile: %v", err)
	}
	if first.ScenesBuilt == 0 {
		t.Fatal("first compile should produce scenes")
	}

	// Second compile without --force should be a no-op (snapshot already committed).
	second, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("second compile: %v", err)
	}
	if second.ScenesBuilt != 0 {
		t.Errorf("second compile should skip committed chapter, got ScenesBuilt=%d", second.ScenesBuilt)
	}
}

type compileTestChapter struct {
	title      string
	paragraphs []string
}

func buildTestProjectWithChapters(t *testing.T, specs []compileTestChapter) (*project.Project, *store.Store) {
	t.Helper()
	dir := t.TempDir()

	p, err := project.Init(dir, project.InitOptions{Title: "Test Novel", Language: "en"})
	if err != nil {
		t.Fatalf("project.Init: %v", err)
	}

	toc := manuscript.TOC{Version: 1}
	for i, spec := range specs {
		chapterID := ids.ChapterID(i + 1)
		ch := &manuscript.Chapter{
			ID:        chapterID,
			Order:     i + 1,
			Title:     spec.title,
			File:      "chapters/" + chapterID + ".md",
			SourceKey: chapterID + ".md",
		}
		for _, text := range spec.paragraphs {
			ch.Blocks = append(ch.Blocks, manuscript.Block{
				Type:        manuscript.BlockParagraph,
				ParagraphID: ids.NewParagraphID(),
				Text:        text,
			})
		}
		if err := manuscript.WriteChapter(p.Path(project.ManuscriptDir), ch); err != nil {
			t.Fatalf("WriteChapter %s: %v", chapterID, err)
		}
		toc.Chapters = append(toc.Chapters, manuscript.TOCEntry{
			ID:        ch.ID,
			Order:     ch.Order,
			Title:     ch.Title,
			File:      ch.File,
			SourceKey: ch.SourceKey,
		})
	}
	if err := manuscript.SaveTOC(p.Path(project.TOCPath), toc); err != nil {
		t.Fatalf("SaveTOC: %v", err)
	}
	if err := store.Rebuild(p); err != nil {
		t.Fatalf("store.Rebuild: %v", err)
	}

	st, err := store.Open(p.Path(project.IndexPath))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return p, st
}

func TestCompileSummariesSendsChaptersOneAtATime(t *testing.T) {
	chapterOneText := "CHAPTER_ONE_ONLY Mara enters the archive."
	chapterTwoText := "CHAPTER_TWO_ONLY Ilya locks the gate."
	p, st := buildTestProjectWithChapters(t, []compileTestChapter{
		{title: "The Archive", paragraphs: []string{chapterOneText}},
		{title: "The Gate", paragraphs: []string{chapterTwoText}},
	})

	ch1Paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter ch-0001: %v", err)
	}
	ch2Paragraphs, err := st.ParagraphsByChapter("ch-0002")
	if err != nil {
		t.Fatalf("ParagraphsByChapter ch-0002: %v", err)
	}
	fake := &fakeProvider{responses: []string{
		`{"summary":"Chapter one summary.","evidence":["` + ch1Paragraphs[0].ID + `"]}`,
		`{"summary":"Chapter two summary.","evidence":["` + ch2Paragraphs[0].ID + `"]}`,
		`{"summary":"Book summary.","evidence":["` + ch1Paragraphs[0].ID + `"]}`,
	}}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile summaries: %v", err)
	}
	if result.SummariesBuilt != 3 {
		t.Fatalf("SummariesBuilt = %d, want 3 (two chapters + book)", result.SummariesBuilt)
	}
	if len(fake.requests) != 3 {
		t.Fatalf("Generate calls = %d, want 3", len(fake.requests))
	}

	firstPrompt := fake.requests[0].Messages[1].Content
	if !strings.Contains(firstPrompt, "Chapter ID: ch-0001") || !strings.Contains(firstPrompt, chapterOneText) {
		t.Fatalf("first chapter prompt missing chapter one content: %s", firstPrompt)
	}
	if strings.Contains(firstPrompt, chapterTwoText) {
		t.Fatalf("first chapter prompt contains chapter two text: %s", firstPrompt)
	}

	secondPrompt := fake.requests[1].Messages[1].Content
	if !strings.Contains(secondPrompt, "Chapter ID: ch-0002") || !strings.Contains(secondPrompt, chapterTwoText) {
		t.Fatalf("second chapter prompt missing chapter two content: %s", secondPrompt)
	}
	if strings.Contains(secondPrompt, chapterOneText) {
		t.Fatalf("second chapter prompt contains chapter one text: %s", secondPrompt)
	}
}

func TestCompileSummariesSplitsOversizedChapterIntoWindows(t *testing.T) {
	firstWindowText := "OVERSIZED_WINDOW_ONE The archive shelves hum with names and dust."
	secondWindowText := "OVERSIZED_WINDOW_TWO The locked gate answers with a silver echo."
	p, st := buildTestProjectWithChapters(t, []compileTestChapter{
		{title: "A Large Chapter", paragraphs: []string{firstWindowText, secondWindowText}},
	})
	p.Config.Compile.TargetContextTokens = 2
	p.Config.Compile.WindowOverlapParagraphs = 0

	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	fake := &fakeProvider{responses: []string{
		`{"summary":"First window summary.","evidence":["` + paragraphs[0].ID + `"]}`,
		`{"summary":"Second window summary.","evidence":["` + paragraphs[1].ID + `"]}`,
		`{"summary":"Chapter synthesis.","evidence":["` + paragraphs[0].ID + `","` + paragraphs[1].ID + `"]}`,
		`{"summary":"Book synthesis.","evidence":["` + paragraphs[0].ID + `"]}`,
	}}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile summaries: %v", err)
	}
	if result.SummariesBuilt != 2 {
		t.Fatalf("SummariesBuilt = %d, want 2 (chapter + book)", result.SummariesBuilt)
	}
	if len(fake.requests) != 4 {
		t.Fatalf("Generate calls = %d, want 4", len(fake.requests))
	}

	firstPrompt := fake.requests[0].Messages[1].Content
	if !strings.Contains(firstPrompt, "Window: 1 of 2") || !strings.Contains(firstPrompt, firstWindowText) {
		t.Fatalf("first window prompt missing first window content: %s", firstPrompt)
	}
	if strings.Contains(firstPrompt, secondWindowText) {
		t.Fatalf("first window prompt contains second window text: %s", firstPrompt)
	}

	secondPrompt := fake.requests[1].Messages[1].Content
	if !strings.Contains(secondPrompt, "Window: 2 of 2") || !strings.Contains(secondPrompt, secondWindowText) {
		t.Fatalf("second window prompt missing second window content: %s", secondPrompt)
	}
	if strings.Contains(secondPrompt, firstWindowText) {
		t.Fatalf("second window prompt contains first window text: %s", secondPrompt)
	}

	synthesisPrompt := fake.requests[2].Messages[1].Content
	if !strings.Contains(synthesisPrompt, "Window summaries:") || !strings.Contains(synthesisPrompt, "First window summary.") || !strings.Contains(synthesisPrompt, "Second window summary.") {
		t.Fatalf("synthesis prompt missing window summaries: %s", synthesisPrompt)
	}
}
func TestCompileSummariesWithFakeProvider(t *testing.T) {
	p, st := buildTestProject(t)

	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	if len(paragraphs) == 0 {
		t.Fatal("expected test chapter paragraphs")
	}

	response := `{"summary":"Mara walks and dawn follows.","themes":["journey"],"unresolved":[],"evidence":["` + paragraphs[0].ID + `"]}`
	fake := &fakeProvider{response: response}
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile summaries: %v", err)
	}
	if result.SummariesBuilt != 2 {
		t.Errorf("SummariesBuilt = %d, want 2 (chapter + book)", result.SummariesBuilt)
	}

	path := p.Path(filepath.Join(project.ModelDir, "summaries.jsonl"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read summaries.jsonl: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"record_type":"chapter_summary"`) {
		t.Fatalf("summaries.jsonl missing chapter_summary record: %s", content)
	}
	if !strings.Contains(content, `"record_type":"book_summary"`) {
		t.Fatalf("summaries.jsonl missing book_summary record: %s", content)
	}
}

func TestCompileSummariesSkipsExisting(t *testing.T) {
	p, st := buildTestProject(t)
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	response := `{"summary":"Mara walks and dawn follows.","evidence":["` + paragraphs[0].ID + `"]}`
	fake := &fakeProvider{response: response}

	first, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("first summaries compile: %v", err)
	}
	if first.SummariesBuilt != 2 {
		t.Fatalf("first SummariesBuilt = %d, want 2", first.SummariesBuilt)
	}

	second, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("second summaries compile: %v", err)
	}
	if second.SummariesBuilt != 0 {
		t.Errorf("second summaries compile should skip existing records, got %d", second.SummariesBuilt)
	}
}

func TestCompileSummariesRequiresProvider(t *testing.T) {
	p, st := buildTestProject(t)
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerSummaries,
	})
	if err == nil {
		t.Fatal("expected error when no provider configured for summaries")
	}
}
