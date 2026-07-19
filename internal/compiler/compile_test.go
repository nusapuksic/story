package compiler_test

import (
	"context"
	"os"
	"path/filepath"
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
