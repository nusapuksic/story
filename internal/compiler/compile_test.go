package compiler_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/manuscript"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/provider"
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
	assertRunPendingFiles(t, p, result.RunID, compiler.LayerScenes, 1)
	assertRunCommitLogSequences(t, p, result.RunID, compiler.LayerScenes, []int{0})

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

func TestCompileWritesRunLog(t *testing.T) {
	p, st := buildTestProject(t)

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerScenes})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	entry := latestRunLogEntry(t, p)
	if entry.RunID != result.RunID {
		t.Fatalf("log run_id = %q, want %q", entry.RunID, result.RunID)
	}
	if entry.RunType != "compile" {
		t.Fatalf("log run_type = %q, want compile", entry.RunType)
	}
	if entry.Status != compiler.RunStatusCompleted {
		t.Fatalf("log status = %q, want completed", entry.Status)
	}
	if entry.Layer != compiler.LayerScenes {
		t.Fatalf("log layer = %q, want scenes", entry.Layer)
	}
	if entry.FinishedAt == "" {
		t.Fatal("log finished_at is empty")
	}
	if entry.Error != "" {
		t.Fatalf("log error = %q, want empty", entry.Error)
	}
}

func TestCompileCanceledMarksRunInterrupted(t *testing.T) {
	p, st := buildTestProject(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := compiler.Compile(ctx, p, st, compiler.Options{Layer: compiler.LayerScenes})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Compile error = %v, want context.Canceled", err)
	}
	if result.RunID == "" {
		t.Fatal("canceled compile did not return a run id")
	}

	runData, err := os.ReadFile(p.Path(filepath.Join(project.RunsDir, result.RunID, "run.json")))
	if err != nil {
		t.Fatalf("read run.json: %v", err)
	}
	var runRecord compiler.RunRecord
	if err := json.Unmarshal(runData, &runRecord); err != nil {
		t.Fatalf("parse run.json: %v", err)
	}
	if runRecord.Status != compiler.RunStatusInterrupted {
		t.Fatalf("run status = %q, want interrupted", runRecord.Status)
	}
	if runRecord.FinishedAt == "" {
		t.Fatal("interrupted run missing finished_at")
	}

	entry := latestRunLogEntry(t, p)
	if entry.RunID != result.RunID {
		t.Fatalf("log run_id = %q, want %q", entry.RunID, result.RunID)
	}
	if entry.Status != compiler.RunStatusInterrupted {
		t.Fatalf("log status = %q, want interrupted", entry.Status)
	}
	if !strings.Contains(entry.Error, context.Canceled.Error()) {
		t.Fatalf("log error = %q, want context canceled", entry.Error)
	}
}

func TestCompileScenesSuggestsSplitForSingleFullChapterScene(t *testing.T) {
	p, st := buildTestProjectWithChapters(t, []compileTestChapter{
		{title: "One Long Scene", paragraphs: []string{
			"Mara enters the archive.",
			"The door closes behind her.",
			"Dust gathers in the green light.",
			"A locked drawer waits beneath the desk.",
			"She hears footsteps in the hall.",
			"The key warms in her hand.",
		}},
	})

	var events []compiler.ProgressEvent
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
		Progress: func(event compiler.ProgressEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if result.ScenesBuilt != 1 {
		t.Fatalf("ScenesBuilt = %d, want 1", result.ScenesBuilt)
	}

	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	wantMidpoint := paragraphs[len(paragraphs)/2-1].ID
	suggestion := ""
	for _, event := range events {
		if event.Layer == compiler.LayerScenes && event.Stage == "suggestion" {
			suggestion = event.Message
			break
		}
	}
	if suggestion == "" {
		t.Fatalf("missing single-scene chapter suggestion: %#v", events)
	}
	for _, want := range []string{
		"one scene spans the full chapter",
		wantMidpoint,
		"story compile --layer scenes --chapter ch-0001 --force",
	} {
		if !strings.Contains(suggestion, want) {
			t.Fatalf("suggestion = %q, want it to contain %q", suggestion, want)
		}
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
	cardJSON := `{"title":"Mara walks","summary":"Mara walks the road.","themes":["threshold"],"entities":["Mara"],"participants":["Mara"],"unresolved":["Why the road?"],"evidence":[]}`
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
	assertRunPendingFiles(t, p, result.RunID, compiler.LayerSceneCards, 2)
	assertRunCommitLogSequences(t, p, result.RunID, compiler.LayerSceneCards, []int{0, 1})

	refs, err := st.ReverseIndexRefs(store.ReverseTermTheme, `threshold`)
	if err != nil {
		t.Fatalf(`ReverseIndexRefs: %v`, err)
	}
	if len(refs) != 2 {
		t.Fatalf(`theme refs = %d, want 2: %#v`, len(refs), refs)
	}
}

func TestCompileSceneCardsKeepsSuccessfulFullChapterScene(t *testing.T) {
	p, st := buildTestProjectWithChapters(t, []compileTestChapter{
		{title: "One Long Scene", paragraphs: []string{
			"Mara enters the archive.",
			"The door closes behind her.",
		}},
	})

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}

	fake := &fakeProvider{response: `{"title":"Archive","summary":"Mara enters the archive.","evidence":[]}`}
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile scene-cards: %v", err)
	}
	if result.CardsBuilt != 1 {
		t.Fatalf("CardsBuilt = %d, want 1", result.CardsBuilt)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("Generate calls = %d, want 1", len(fake.requests))
	}
	cards, err := st.AllSceneCards()
	if err != nil {
		t.Fatalf("AllSceneCards: %v", err)
	}
	if len(cards) != 1 || cards[0].Title != "Archive" {
		t.Fatalf("scene cards = %#v, want successful Archive card", cards)
	}
}

func TestCompileSceneCardsSkipsOversizedFullChapterSceneAfterInitialFailure(t *testing.T) {
	p, st := buildTestProjectWithChapters(t, []compileTestChapter{
		{title: "One Long Scene", paragraphs: []string{
			"Mara enters the archive.",
			"The door closes behind her.",
		}},
	})

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}

	firstFake := &fakeProvider{response: `{"title":"Archive","summary":"Mara enters the archive.","evidence":[]}`}
	first, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		ExtractionProvider: firstFake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile initial scene-card: %v", err)
	}
	if first.CardsBuilt != 1 {
		t.Fatalf("initial CardsBuilt = %d, want 1", first.CardsBuilt)
	}

	p.Config.Compile.TargetContextTokens = 1
	fake := &fakeProvider{response: `{"title":"Bad","summary":"Bad citations.","evidence":["p-NONEXISTENT"]}`}
	var events []compiler.ProgressEvent
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		Force:              true,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
		Progress: func(event compiler.ProgressEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("compile scene-cards: %v", err)
	}
	if result.CardsBuilt != 0 {
		t.Fatalf("CardsBuilt = %d, want 0", result.CardsBuilt)
	}
	if result.SceneCardRecoveries != 0 {
		t.Fatalf("SceneCardRecoveries = %d, want 0", result.SceneCardRecoveries)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("Generate calls = %d, want 1", len(fake.requests))
	}
	cards, err := st.AllSceneCards()
	if err != nil {
		t.Fatalf("AllSceneCards: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("scene cards = %d, want 0", len(cards))
	}
	if !progressEventsContain(events, compiler.LayerSceneCards, "item-start", "trying once without retry/fallback") {
		t.Fatalf("progress events missing oversized one-shot notice: %#v", events)
	}
	if !progressEventsContain(events, compiler.LayerSceneCards, "item-skip", "skipped after initial failure") {
		t.Fatalf("progress events missing full-chapter failure skip: %#v", events)
	}
	scenesJSONL, err := os.ReadFile(p.Path(filepath.Join(project.ModelDir, "scenes.jsonl")))
	if err != nil {
		t.Fatalf("read scenes.jsonl: %v", err)
	}
	if !strings.Contains(string(scenesJSONL), `"status":"skipped"`) || !strings.Contains(string(scenesJSONL), `"action":"skipped"`) {
		t.Fatalf("scenes.jsonl missing skipped scene-card marker:\n%s", scenesJSONL)
	}
}

func TestCompileReportsSceneCardProgress(t *testing.T) {
	p, st := buildTestProject(t)

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}

	fake := &fakeProvider{response: `{"title":"Mara walks","summary":"Mara walks the road.","evidence":[]}`}
	var events []compiler.ProgressEvent
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
		Progress: func(event compiler.ProgressEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("compile scene-cards: %v", err)
	}

	for _, want := range []struct {
		stage string
		text  string
	}{
		{stage: "layer-start", text: "Scene cards: starting"},
		{stage: "item-start", text: "extracting from"},
		{stage: "item-complete", text: "completed"},
		{stage: "layer-complete", text: "Scene cards: completed"},
	} {
		if !progressEventsContain(events, compiler.LayerSceneCards, want.stage, want.text) {
			t.Fatalf("progress events missing %s/%q: %#v", want.stage, want.text, events)
		}
	}
}

func TestCompileReportsSceneCardCompletionBeforeChapterFinishes(t *testing.T) {
	p, st := buildTestProject(t)

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}

	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	completionEvents := make(chan string, 2)
	release := func() {
		select {
		case <-releaseSecond:
		default:
			close(releaseSecond)
		}
	}
	defer release()

	fake := &fakeProvider{responseFunc: func(req provider.GenerationRequest, idx int) string {
		if idx == 1 {
			close(secondStarted)
			<-releaseSecond
		}
		return `{"title":"Mara walks","summary":"Mara walks the road.","evidence":[]}`
	}}

	done := make(chan error, 1)
	go func() {
		_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
			Layer:              compiler.LayerSceneCards,
			ExtractionProvider: fake,
			ExtractionModel:    "fake-model",
			Progress: func(event compiler.ProgressEvent) {
				if event.Layer == compiler.LayerSceneCards && event.Stage == "item-complete" {
					completionEvents <- event.SceneID
				}
			},
		})
		done <- err
	}()

	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second scene extraction did not start")
	}

	select {
	case err := <-done:
		t.Fatalf("compile finished before second scene was released: %v", err)
	default:
	}

	select {
	case sceneID := <-completionEvents:
		if sceneID == "" {
			t.Fatal("first scene-card completion event had no scene id")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("first scene-card completion was not reported while the chapter was still running")
	}

	release()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("compile scene-cards: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("compile scene-cards did not finish")
	}
}
func principalRoleResponseFromPrompt(t *testing.T, prompt string) string {
	t.Helper()
	entityID := promptIDAfter(t, prompt, "Character entity records:", "entity-")
	sceneID := promptIDAfter(t, prompt, "Linked scene evidence:", "sc-")
	return `{"characters":[{"source_entity_ids":["` + entityID + `"],"classification":"principal","role":"protagonist","confidence":0.94,"rationale":"Mara drives the central action.","evidence":[{"scene_id":"` + sceneID + `","reason":"Shows Mara carrying the scene action."}]}]}`
}

func bookSummaryWithFinalStateFromPrompt(t *testing.T, prompt, summary, state string) string {
	t.Helper()
	characterID := promptIDAfter(t, prompt, "Principal characters requiring final-state coverage:", "char-")
	return `{"summary":"` + summary + `","evidence":["ch-0001"],"character_final_states":[{"character_id":"` + characterID + `","state":"` + state + `"}]}`
}

func promptIDAfter(t *testing.T, prompt, marker, prefix string) string {
	t.Helper()
	start := strings.Index(prompt, marker)
	if start < 0 {
		t.Fatalf("prompt missing marker %q:\n%s", marker, prompt)
	}
	idx := strings.Index(prompt[start:], prefix)
	if idx < 0 {
		t.Fatalf("prompt section %q missing id prefix %q:\n%s", marker, prefix, prompt[start:])
	}
	idx += start
	end := idx
	for end < len(prompt) && isPromptIDByte(prompt[end]) {
		end++
	}
	return prompt[idx:end]
}

func isPromptIDByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-'
}

func progressEventsContain(events []compiler.ProgressEvent, layer, stage, text string) bool {
	for _, event := range events {
		if event.Layer == layer && event.Stage == stage && strings.Contains(event.Message, text) {
			return true
		}
	}
	return false
}

type runLogEntry struct {
	RunID      string `json:"run_id"`
	RunType    string `json:"run_type"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Status     string `json:"status"`
	Layer      string `json:"layer"`
	ChapterID  string `json:"chapter_id"`
	Error      string `json:"error"`
}

func latestRunLogEntry(t *testing.T, p *project.Project) runLogEntry {
	t.Helper()
	data, err := os.ReadFile(p.Path(filepath.Join(project.LogsDir, "runs.jsonl")))
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("run log is empty")
	}
	var entry runLogEntry
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("parse run log: %v", err)
	}
	return entry
}

func assertRunPendingFiles(t *testing.T, p *project.Project, runID, layer string, want int) {
	t.Helper()
	dir := p.Path(filepath.Join(project.RunsDir, runID, "pending", layer))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read pending %s: %v", layer, err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		count++
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read pending file %s: %v", entry.Name(), err)
		}
		if !strings.Contains(string(data), `"payload"`) || !strings.Contains(string(data), `"content_hash"`) {
			t.Fatalf("pending file %s missing payload/content_hash:\n%s", entry.Name(), data)
		}
	}
	if count != want {
		t.Fatalf("pending %s files = %d, want %d", layer, count, want)
	}
}

func assertRunCommitLogSequences(t *testing.T, p *project.Project, runID, layer string, want []int) {
	t.Helper()
	data, err := os.ReadFile(p.Path(filepath.Join(project.RunsDir, runID, "commit-log.jsonl")))
	if err != nil {
		t.Fatalf("read commit-log.jsonl: %v", err)
	}
	var got []int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			Sequence int    `json:"sequence"`
			Layer    string `json:"layer"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("parse commit log line %q: %v", line, err)
		}
		if entry.Layer == layer {
			got = append(got, entry.Sequence)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("commit log %s sequences = %v, want %v", layer, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("commit log %s sequences = %v, want %v", layer, got, want)
		}
	}
}

func assertRunPendingPayloadOmits(t *testing.T, p *project.Project, runID, layer string, forbidden ...string) {
	t.Helper()
	dir := p.Path(filepath.Join(project.RunsDir, runID, "pending", layer))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read pending %s: %v", layer, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read pending file %s: %v", entry.Name(), err)
		}
		content := string(data)
		for _, value := range forbidden {
			if strings.Contains(content, value) {
				t.Fatalf("pending file %s contains forbidden %s:\n%s", entry.Name(), value, data)
			}
		}
	}
}

func TestCompileSceneCardsSkipsTruncatedJSONInsteadOfSavingFallback(t *testing.T) {
	p, st := buildTestProject(t)

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	if len(paragraphs) < 2 {
		t.Fatalf("expected two paragraphs, got %d", len(paragraphs))
	}

	truncated := `{"title":"Unfinished","summary":"Mara starts`
	fake := &fakeProvider{responses: []string{
		truncated,
		truncated,
		`{"title":"Sunrise","summary":"The sun rises over the hills.","evidence":["` + paragraphs[1].ID + `"]}`,
	}}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile scene-cards should skip truncated JSON: %v", err)
	}
	if result.CardsBuilt != 1 {
		t.Fatalf("CardsBuilt = %d, want 1 valid card", result.CardsBuilt)
	}
	if result.SceneCardRecoveries != 0 {
		t.Fatalf("SceneCardRecoveries = %d, want 0 for skipped truncated output", result.SceneCardRecoveries)
	}
	if len(fake.requests) != 3 {
		t.Fatalf("Generate calls = %d, want 3", len(fake.requests))
	}

	cards, err := st.AllSceneCards()
	if err != nil {
		t.Fatalf("AllSceneCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want only the valid second scene card", len(cards))
	}
	if cards[0].SceneID == "" || len(cards[0].Evidence) != 1 || cards[0].Evidence[0] != paragraphs[1].ID {
		t.Fatalf("stored card = %#v, want valid second scene card", cards[0])
	}

	scenesJSONL, err := os.ReadFile(p.Path(filepath.Join(project.ModelDir, "scenes.jsonl")))
	if err != nil {
		t.Fatalf("read scenes.jsonl: %v", err)
	}
	for _, want := range []string{`"status":"skipped"`, `"action":"skipped"`, `"attempts":2`, `truncated model JSON after retry`} {
		if !strings.Contains(string(scenesJSONL), want) {
			t.Fatalf("scenes.jsonl missing %q:\n%s", want, scenesJSONL)
		}
	}
}
func TestCompileSceneCardsRecoversInvalidEvidence(t *testing.T) {
	p, st := buildTestProject(t)

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	if len(paragraphs) < 2 {
		t.Fatalf("expected two paragraphs, got %d", len(paragraphs))
	}

	invalid := `{"title":"Bad","summary":"Bad citations.","evidence":["p-NONEXISTENT"]}`
	fake := &fakeProvider{responses: []string{
		invalid,
		invalid,
		`{"title":"Sunrise","summary":"The sun rises over the hills.","evidence":["` + paragraphs[1].ID + `"]}`,
	}}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile scene-cards should recover invalid evidence: %v", err)
	}
	if result.CardsBuilt != 2 {
		t.Fatalf("CardsBuilt = %d, want 2", result.CardsBuilt)
	}
	if result.SceneCardRecoveries != 1 {
		t.Fatalf("SceneCardRecoveries = %d, want 1", result.SceneCardRecoveries)
	}
	if len(result.SceneCardRecoveryEvents) != 1 {
		t.Fatalf("SceneCardRecoveryEvents = %d, want 1", len(result.SceneCardRecoveryEvents))
	}
	if result.SceneCardRecoveryEvents[0].ChapterID != "ch-0001" || result.SceneCardRecoveryEvents[0].Action != "fallback" {
		t.Fatalf("SceneCardRecoveryEvents[0] = %#v, want ch-0001 fallback", result.SceneCardRecoveryEvents[0])
	}
	if len(fake.requests) != 3 {
		t.Fatalf("Generate calls = %d, want 3", len(fake.requests))
	}

	cards, err := st.AllSceneCards()
	if err != nil {
		t.Fatalf("AllSceneCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("cards = %d, want 2", len(cards))
	}
	if len(cards[0].Evidence) != 1 || cards[0].Evidence[0] != paragraphs[0].ID {
		t.Fatalf("fallback evidence = %v, want [%s]", cards[0].Evidence, paragraphs[0].ID)
	}
	if !strings.Contains(cards[0].RawJSON, `"action":"fallback"`) {
		t.Fatalf("fallback card raw JSON missing recovery block: %s", cards[0].RawJSON)
	}

	summary, err := os.ReadFile(p.Path(filepath.Join(project.RunsDir, result.RunID, "summary.json")))
	if err != nil {
		t.Fatalf("read summary.json: %v", err)
	}
	for _, want := range []string{`"scene_card_recovery_events"`, `"scene_id": "` + result.SceneCardRecoveryEvents[0].SceneID + `"`, `"chapter_id": "ch-0001"`, `"action": "fallback"`} {
		if !strings.Contains(string(summary), want) {
			t.Fatalf("summary.json missing %s:\n%s", want, summary)
		}
	}
}

func TestCompileSceneCardsRecoversTimeoutWithFallback(t *testing.T) {
	p, st := buildTestProject(t)

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	if len(paragraphs) < 2 {
		t.Fatalf("expected two paragraphs, got %d", len(paragraphs))
	}

	fake := &fakeProvider{
		responses: []string{
			"",
			"",
			`{"title":"Sunrise","summary":"The sun rises over the hills.","evidence":["` + paragraphs[1].ID + `"]}`,
		},
		errors: []error{context.DeadlineExceeded, context.DeadlineExceeded, nil},
	}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile scene-cards should recover timeout: %v", err)
	}
	if result.CardsBuilt != 2 {
		t.Fatalf("CardsBuilt = %d, want 2", result.CardsBuilt)
	}
	if result.SceneCardRecoveries != 1 {
		t.Fatalf("SceneCardRecoveries = %d, want 1", result.SceneCardRecoveries)
	}
	if len(result.SceneCardRecoveryEvents) != 1 {
		t.Fatalf("SceneCardRecoveryEvents = %d, want 1", len(result.SceneCardRecoveryEvents))
	}
	if result.SceneCardRecoveryEvents[0].ChapterID != "ch-0001" || result.SceneCardRecoveryEvents[0].Action != "fallback" {
		t.Fatalf("SceneCardRecoveryEvents[0] = %#v, want ch-0001 fallback", result.SceneCardRecoveryEvents[0])
	}
	if len(fake.requests) != 3 {
		t.Fatalf("Generate calls = %d, want 3", len(fake.requests))
	}

	cards, err := st.AllSceneCards()
	if err != nil {
		t.Fatalf("AllSceneCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("cards = %d, want 2", len(cards))
	}
	if len(cards[0].Evidence) != 1 || cards[0].Evidence[0] != paragraphs[0].ID {
		t.Fatalf("fallback evidence = %v, want [%s]", cards[0].Evidence, paragraphs[0].ID)
	}
	if !strings.Contains(cards[0].RawJSON, `"action":"fallback"`) {
		t.Fatalf("timeout fallback raw JSON missing recovery block: %s", cards[0].RawJSON)
	}
}

func TestCompileSceneCardsStrictInvalidEvidenceFails(t *testing.T) {
	p, st := buildTestProject(t)

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer: compiler.LayerScenes,
	})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}
	fake := &fakeProvider{response: `{"title":"Bad","summary":"Bad citations.","evidence":["p-NONEXISTENT"]}`}

	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:                  compiler.LayerSceneCards,
		SceneCardFailurePolicy: compiler.SceneCardFailurePolicyStrict,
		ExtractionProvider:     fake,
		ExtractionModel:        "fake-model",
	})
	if err == nil {
		t.Fatal("expected strict scene-card compile to fail")
	}
	if !strings.Contains(err.Error(), "unknown paragraph ID") {
		t.Fatalf("error = %v, want unknown paragraph ID", err)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("Generate calls = %d, want 1 in strict mode", len(fake.requests))
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

func TestCompileAllRunsEntitiesBeforeSummaries(t *testing.T) {
	p, st := buildTestProject(t)
	p.Config.Compile.SceneDetection = "explicit"
	p.Config.Compile.Verification = false
	p.Config.Compile.VerificationMode = compiler.VerificationModeOff

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerScenes})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	scenes, err := st.ScenesByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ScenesByChapter: %v", err)
	}
	if len(paragraphs) == 0 || len(scenes) == 0 {
		t.Fatal("expected chapter paragraphs and scenes")
	}

	cardJSON := `{"title":"Scene","summary":"Scene summary.","entities":["Mara"],"participants":["Mara"],"evidence":[]}`
	chapterSummaryJSON := `{"summary":"Chapter summary.","evidence":["` + paragraphs[0].ID + `"]}`
	entitiesJSON := `{"entities":[{"canonical_name":"Mara","type":"character","occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara"],"confidence":0.95}]}]}`
	fake := &fakeProvider{responseFunc: func(req provider.GenerationRequest, idx int) string {
		prompt := req.Messages[1].Content
		switch {
		case strings.Contains(prompt, "Extract a structured scene card"):
			return cardJSON
		case strings.Contains(prompt, "Consolidate candidate entities"):
			return entitiesJSON
		case strings.Contains(prompt, "Classify book-level character roles"):
			return principalRoleResponseFromPrompt(t, prompt)
		case strings.Contains(prompt, "Summarize this chapter"):
			return chapterSummaryJSON
		case strings.Contains(prompt, "Produce a comprehensive editorial synopsis"):
			return bookSummaryWithFinalStateFromPrompt(t, prompt, "Book summary. Mara ends watchful and steady.", "Mara is watchful and steady.")
		default:
			t.Fatalf("unexpected prompt: %s", prompt)
			return `{}`
		}
	}}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile all layers: %v", err)
	}
	if result.CardsBuilt != 2 || result.SummariesBuilt != 2 || result.EntitiesBuilt != 1 || result.PrincipalsBuilt != 1 {
		t.Fatalf("built counts = cards %d, summaries %d, entities %d, principals %d; want 2, 2, 1, 1",
			result.CardsBuilt, result.SummariesBuilt, result.EntitiesBuilt, result.PrincipalsBuilt)
	}
	if len(fake.requests) != 6 {
		t.Fatalf("Generate calls = %d, want 6", len(fake.requests))
	}

	wantPrompts := []string{
		"Extract a structured scene card",
		"Extract a structured scene card",
		"Consolidate candidate entities",
		"Classify book-level character roles",
		"Summarize this chapter",
		"Produce a comprehensive editorial synopsis",
	}
	for i, want := range wantPrompts {
		prompt := fake.requests[i].Messages[1].Content
		if !strings.Contains(prompt, want) {
			t.Fatalf("request %d prompt = %q, want it to contain %q", i, prompt, want)
		}
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
		`{"summary":"Book summary.","evidence":["ch-0001","ch-0002"]}`,
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
	assertRunPendingFiles(t, p, result.RunID, compiler.LayerSummaries, 3)
	assertRunCommitLogSequences(t, p, result.RunID, compiler.LayerSummaries, []int{0, 1, 2})
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

func TestCompileBookSummaryUsesChapterSummariesOnly(t *testing.T) {
	chapterOneText := "Chapter one opens the shore."
	chapterTwoOpening := "Chapter two begins before sunrise."
	chapterTwoEvidence := "The drowned village was not destroyed, merely covered."
	p, st := buildTestProjectWithChapters(t, []compileTestChapter{
		{title: "The Shore", paragraphs: []string{chapterOneText}},
		{title: "The Drowned Street", paragraphs: []string{chapterTwoOpening, chapterTwoEvidence}},
	})

	ch1Paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter ch-0001: %v", err)
	}
	ch2Paragraphs, err := st.ParagraphsByChapter("ch-0002")
	if err != nil {
		t.Fatalf("ParagraphsByChapter ch-0002: %v", err)
	}

	summaries := `{"record_type":"chapter_summary","chapter_id":"ch-0001","summary":"Chapter one opens the shore.","evidence":["` + ch1Paragraphs[0].ID + `"],"generation":{"prompt_version":"chapter-summary-v1"},"status":"generated"}` + "\n" +
		`{"record_type":"chapter_summary","chapter_id":"ch-0002","summary":"The drowned village was merely covered [` + ch2Paragraphs[1].ID + `].","evidence":[],"generation":{"prompt_version":"chapter-summary-v1"},"status":"generated"}` + "\n"
	path := p.Path(filepath.Join(project.ModelDir, "summaries.jsonl"))
	if err := os.WriteFile(path, []byte(summaries), 0o644); err != nil {
		t.Fatalf("write existing summaries: %v", err)
	}

	fake := &fakeProvider{response: `{"summary":"Book summary cites both chapters.","evidence":["ch-0001","ch-0002"]}`}
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile summaries: %v", err)
	}
	if result.SummariesBuilt != 1 {
		t.Fatalf("SummariesBuilt = %d, want 1 book summary", result.SummariesBuilt)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("Generate calls = %d, want 1", len(fake.requests))
	}
	bookPrompt := fake.requests[0].Messages[1].Content
	if strings.Contains(bookPrompt, "Supporting paragraphs:") {
		t.Fatalf("book prompt should not include supporting paragraph excerpts: %s", bookPrompt)
	}
	if strings.Contains(bookPrompt, chapterTwoEvidence) {
		t.Fatalf("book prompt includes source paragraph text instead of only chapter summaries: %s", bookPrompt)
	}
	if strings.Contains(bookPrompt, ch2Paragraphs[1].ID) {
		t.Fatalf("book prompt should strip paragraph citations from chapter summaries: %s", bookPrompt)
	}
	if !strings.Contains(bookPrompt, "The drowned village was merely covered.") {
		t.Fatalf("book prompt missing sanitized chapter summary: %s", bookPrompt)
	}
}

func TestCompileChapterSummaryIgnoresNonParagraphInlinePhrases(t *testing.T) {
	chapterText := "The archive note includes a statistical p-value but no extra citation."
	p, st := buildTestProjectWithChapters(t, []compileTestChapter{
		{title: "The Archive", paragraphs: []string{chapterText}},
	})
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}

	fake := &fakeProvider{response: `{"summary":"The archive note mentions a p-value but cites [` + paragraphs[0].ID + `].","evidence":[]}`}
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ChapterID:          "ch-0001",
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile summaries: %v", err)
	}

	data, err := os.ReadFile(p.Path(filepath.Join(project.ModelDir, "summaries.jsonl")))
	if err != nil {
		t.Fatalf("read summaries.jsonl: %v", err)
	}
	var rec struct {
		Evidence []string `json:"evidence"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("parse summary record: %v", err)
	}
	if len(rec.Evidence) != 1 || rec.Evidence[0] != paragraphs[0].ID {
		t.Fatalf("Evidence = %v, want [%s]", rec.Evidence, paragraphs[0].ID)
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
		`{"summary":"Book synthesis.","evidence":["ch-0001"]}`,
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

	chapterResponse := `{"summary":"Mara walks and dawn follows.","themes":["journey"],"unresolved":[],"evidence":[{"paragraph_id":"` + paragraphs[0].ID + `"}]}`
	fake := &fakeProvider{responses: []string{
		chapterResponse,
		`{"summary":"Book summary.","evidence":["ch-0001"]}`,
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

func TestCompileSummariesRunsEntitiesPrerequisiteFirst(t *testing.T) {
	p, st := buildTestProject(t)

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerScenes})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}

	sceneCardProvider := &fakeProvider{response: `{"title":"Road","summary":"Mara walks the road.","entities":["Mara"],"participants":["Mara"],"evidence":[]}`}
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		ExtractionProvider: sceneCardProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile scene cards: %v", err)
	}

	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	scenes, err := st.ScenesByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ScenesByChapter: %v", err)
	}
	if len(paragraphs) == 0 || len(scenes) == 0 {
		t.Fatal("expected chapter paragraphs and scenes")
	}

	entitiesJSON := `{"entities":[{"canonical_name":"Mara","type":"character","occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara"],"confidence":0.9}]}]}`
	chapterSummaryJSON := `{"summary":"Mara walks and dawn follows.","evidence":["` + paragraphs[0].ID + `"]}`
	fake := &fakeProvider{responseFunc: func(req provider.GenerationRequest, idx int) string {
		prompt := req.Messages[1].Content
		switch {
		case strings.Contains(prompt, "Consolidate candidate entities"):
			return entitiesJSON
		case strings.Contains(prompt, "Classify book-level character roles"):
			return principalRoleResponseFromPrompt(t, prompt)
		case strings.Contains(prompt, "Summarize this chapter"):
			return chapterSummaryJSON
		case strings.Contains(prompt, "Produce a comprehensive editorial synopsis"):
			return bookSummaryWithFinalStateFromPrompt(t, prompt, "Mara's final state is cautious but resolved at dawn.", "Mara is cautious but resolved at dawn.")
		default:
			t.Fatalf("unexpected prompt: %s", prompt)
			return `{}`
		}
	}}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile summaries: %v", err)
	}
	if result.SummariesBuilt != 2 || result.PrincipalsBuilt != 1 {
		t.Fatalf("built counts = summaries %d, principals %d; want 2, 1", result.SummariesBuilt, result.PrincipalsBuilt)
	}
	if len(fake.requests) != 4 {
		t.Fatalf("Generate calls = %d, want 4 (entities + principals prerequisites + chapter + book)", len(fake.requests))
	}
	if !strings.Contains(fake.requests[0].Messages[1].Content, "Consolidate candidate entities from scene-card reverse-index results") {
		t.Fatalf("first request should be entity consolidation, got: %s", fake.requests[0].Messages[1].Content)
	}
	if !strings.Contains(fake.requests[1].Messages[1].Content, "Classify book-level character roles") {
		t.Fatalf("second request should be principal classification, got: %s", fake.requests[1].Messages[1].Content)
	}
	if !strings.Contains(fake.requests[2].Messages[1].Content, "Summarize this chapter as evidence-backed JSON.") {
		t.Fatalf("third request should be chapter summary, got: %s", fake.requests[2].Messages[1].Content)
	}
	bookPrompt := fake.requests[3].Messages[1].Content
	if !strings.Contains(bookPrompt, "Principal characters requiring final-state coverage:") || !strings.Contains(bookPrompt, "char-") || !strings.Contains(bookPrompt, "Mara") {
		t.Fatalf("book summary prompt missing character-role artifact requirements: %s", bookPrompt)
	}
}

func TestCompileSummariesFailsWhenBookSummaryMissesPrincipalFinalState(t *testing.T) {
	p, st := buildTestProject(t)

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerScenes})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}
	sceneCardProvider := &fakeProvider{response: `{"title":"Road","summary":"Mara walks the road.","entities":["Mara"],"participants":["Mara"],"evidence":[]}`}
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		ExtractionProvider: sceneCardProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile scene cards: %v", err)
	}
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	scenes, err := st.ScenesByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ScenesByChapter: %v", err)
	}
	entitiesProvider := &fakeProvider{response: `{"entities":[{"canonical_name":"Mara","type":"character","occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara"],"confidence":0.9}]}]}`}
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: entitiesProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities: %v", err)
	}
	principalsProvider := &fakeProvider{responseFunc: func(req provider.GenerationRequest, idx int) string {
		return principalRoleResponseFromPrompt(t, req.Messages[1].Content)
	}}
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerPrincipals,
		ExtractionProvider: principalsProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile principals: %v", err)
	}

	fake := &fakeProvider{responses: []string{
		`{"summary":"Mara walks and dawn follows.","evidence":["` + paragraphs[0].ID + `"]}`,
		`{"summary":"A road story reaches dawn.","evidence":["ch-0001"]}`,
	}}
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err == nil {
		t.Fatal("expected error when book summary omits principal character final state")
	}
	if !strings.Contains(err.Error(), "missing final state for principal character char-") {
		t.Fatalf("error = %q, want missing principal character ID detail", err.Error())
	}
}

func TestCompileSummariesDoesNotInferPrincipalsFromPOVWithoutCharacterEntity(t *testing.T) {
	p, st := buildTestProject(t)

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerScenes})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}

	sceneCardProvider := &fakeProvider{response: `{"title":"Road","summary":"Mara walks the road.","pov":["Mara"],"participants":["Mara"],"evidence":[]}`}
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		ExtractionProvider: sceneCardProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile scene cards: %v", err)
	}

	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	fake := &fakeProvider{responses: []string{
		`{"entities":[]}`,
		`{"summary":"Mara walks and dawn follows.","evidence":["` + paragraphs[0].ID + `"]}`,
		`{"summary":"Mara ends the chapter alert and determined.","evidence":["ch-0001"]}`,
	}}
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile summaries: %v", err)
	}
	if len(fake.requests) != 3 {
		t.Fatalf("Generate calls = %d, want 3", len(fake.requests))
	}
	bookPrompt := fake.requests[2].Messages[1].Content
	if !strings.Contains(bookPrompt, "- (none in character_roles artifact)") {
		t.Fatalf("book summary prompt should not infer principals from POV-only aliases: %s", bookPrompt)
	}
}

func TestCompileSummariesFallsBackOnTruncatedChapterResponse(t *testing.T) {
	p, st := buildTestProject(t)
	fake := &fakeProvider{response: `{"summary":"Mara walks`}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ChapterID:          "ch-0001",
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile summaries: %v", err)
	}
	if result.SummariesBuilt != 1 {
		t.Fatalf("SummariesBuilt = %d, want 1", result.SummariesBuilt)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("Generate calls = %d, want 1", len(fake.requests))
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
	if !strings.Contains(content, "Mara walked the road.") {
		t.Fatalf("fallback summary did not use chapter text: %s", content)
	}
}

func TestCompileSummariesAllowsUncappedOutputTokens(t *testing.T) {
	p, st := buildTestProject(t)
	p.Config.Compile.MaximumOutputTokens = 0

	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	fake := &fakeProvider{response: `{"summary":"Mara walks and dawn follows.","evidence":["` + paragraphs[0].ID + `"]}`}

	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ChapterID:          "ch-0001",
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile summaries: %v", err)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("Generate calls = %d, want 1", len(fake.requests))
	}
	if fake.requests[0].MaxTokens != 0 {
		t.Fatalf("MaxTokens = %d, want 0 for uncapped output", fake.requests[0].MaxTokens)
	}
}

func TestCompileSummariesSkipsExisting(t *testing.T) {
	p, st := buildTestProject(t)
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	chapterResponse := `{"summary":"Mara walks and dawn follows.","evidence":["` + paragraphs[0].ID + `"]}`
	fake := &fakeProvider{responses: []string{
		chapterResponse,
		`{"summary":"Book summary.","evidence":["ch-0001"]}`,
	}}

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

func seedEntityReverseIndex(t *testing.T, p *project.Project, st *store.Store) []store.SceneRow {
	t.Helper()
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerScenes})
	if err != nil {
		t.Fatalf("compile scenes for entity seed: %v", err)
	}
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	scenes, err := st.ScenesByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ScenesByChapter: %v", err)
	}
	if len(paragraphs) == 0 || len(scenes) == 0 {
		t.Fatal("expected paragraphs and scenes for entity seed")
	}
	rawCard := `{"record_type":"scene_card","scene_id":"` + scenes[0].ID + `","title":"Bell warning","summary":"Mara meets Old Petar near the lake.","entities":["Mara","Maraa"],"pov":["Mara"],"participants":["Mara","Old Petar"],"locations":["Lake edge"],"evidence":["` + paragraphs[0].ID + `"],"generation":{},"status":"generated"}`
	if err := st.InsertSceneCard(store.SceneCardRow{
		SceneID:         scenes[0].ID,
		Title:           "Bell warning",
		Summary:         "Mara meets Old Petar near the lake.",
		Evidence:        []string{paragraphs[0].ID},
		GenerationRun:   "compile-test",
		GenerationModel: "test-model",
		PromptVersion:   "scene-extraction-v1",
		Status:          "generated",
		RawJSON:         rawCard,
	}); err != nil {
		t.Fatalf("InsertSceneCard: %v", err)
	}
	if err := st.RebuildReverseIndex(); err != nil {
		t.Fatalf("RebuildReverseIndex: %v", err)
	}
	return scenes
}

func seedSecondSceneCrewReverseIndex(t *testing.T, p *project.Project, st *store.Store, scenes []store.SceneRow) {
	t.Helper()
	if len(scenes) < 2 {
		t.Fatalf("expected at least 2 scenes, got %d", len(scenes))
	}
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	if len(paragraphs) < 2 {
		t.Fatalf("expected at least 2 paragraphs, got %d", len(paragraphs))
	}
	rawCard := `{"record_type":"scene_card","scene_id":"` + scenes[1].ID + `","title":"Crew threat","summary":"The good-for-nothing crew blocks the road.","entities":["good-for-nothing crew"],"participants":["good-for-nothing crew"],"evidence":["` + paragraphs[1].ID + `"],"generation":{},"status":"generated"}`
	if err := st.InsertSceneCard(store.SceneCardRow{
		SceneID:         scenes[1].ID,
		Title:           "Crew threat",
		Summary:         "The good-for-nothing crew blocks the road.",
		Evidence:        []string{paragraphs[1].ID},
		GenerationRun:   "compile-test",
		GenerationModel: "test-model",
		PromptVersion:   "scene-extraction-v1",
		Status:          "generated",
		RawJSON:         rawCard,
	}); err != nil {
		t.Fatalf("InsertSceneCard second scene: %v", err)
	}
	if err := st.RebuildReverseIndex(); err != nil {
		t.Fatalf("RebuildReverseIndex second scene: %v", err)
	}
}

func TestCompileEntitiesWithFakeProvider(t *testing.T) {
	p, st := buildTestProject(t)
	scenes := seedEntityReverseIndex(t, p, st)

	fake := &fakeProvider{response: `{"entities":[{"canonical_name":"Mara","type":"character","aliases":["Maraa"],"occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara","Maraa"],"confidence":0.95,"flags":[{"type":"possible_typo","value":"Maraa","suggested":"Mara","confidence":0.8}]}],"flags":[{"type":"possible_typo","value":"Maraa","suggested":"Mara","confidence":0.8}]}]}`}
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities: %v", err)
	}
	if result.EntitiesBuilt != 1 {
		t.Fatalf("EntitiesBuilt = %d, want 1", result.EntitiesBuilt)
	}
	assertRunPendingFiles(t, p, result.RunID, compiler.LayerEntities, 1)
	assertRunCommitLogSequences(t, p, result.RunID, compiler.LayerEntities, []int{0})
	assertRunPendingPayloadOmits(t, p, result.RunID, compiler.LayerEntities, `"entity_id"`, `"record_type"`)
	entities, occurrences, err := st.EntityCounts()
	if err != nil {
		t.Fatalf("EntityCounts: %v", err)
	}
	if entities != 1 || occurrences != 1 {
		t.Fatalf("entity counts = (%d, %d), want (1, 1)", entities, occurrences)
	}

	entitiesData, err := os.ReadFile(p.Path(filepath.Join(project.ModelDir, "entities.jsonl")))
	if err != nil {
		t.Fatalf("read entities.jsonl: %v", err)
	}
	if !strings.Contains(string(entitiesData), `"record_type":"entity_snapshot"`) {
		t.Fatalf("entities.jsonl missing entity snapshot record: %s", entitiesData)
	}

	data, err := os.ReadFile(p.Path(filepath.Join(project.ModelDir, "occurrences.jsonl")))
	if err != nil {
		t.Fatalf("read occurrences.jsonl: %v", err)
	}
	if !strings.Contains(string(data), `"record_type":"occurrence"`) {
		t.Fatalf("occurrences.jsonl missing occurrence record: %s", data)
	}
	if !strings.Contains(string(data), `"surface_texts":["Mara","Maraa"]`) {
		t.Fatalf("occurrences.jsonl missing consolidated surfaces: %s", data)
	}
}

func TestCompileEntitiesDropsUnsupportedSceneScopedOccurrence(t *testing.T) {
	p, st := buildTestProject(t)
	scenes := seedEntityReverseIndex(t, p, st)
	seedSecondSceneCrewReverseIndex(t, p, st, scenes)

	fake := &fakeProvider{response: `{"entities":[{"canonical_name":"good-for-nothing crew","type":"group","occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["good-for-nothing crew"],"confidence":0.9}]}]}`}
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities with unsupported occurrence: %v", err)
	}
	if result.EntitiesBuilt != 0 {
		t.Fatalf("EntitiesBuilt = %d, want 0 unsupported entities", result.EntitiesBuilt)
	}

	entities, occurrences, err := st.EntityCounts()
	if err != nil {
		t.Fatalf("EntityCounts: %v", err)
	}
	if entities != 0 || occurrences != 0 {
		t.Fatalf("entity counts = (%d, %d), want (0, 0)", entities, occurrences)
	}
	committed, err := st.IsEntitySnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsEntitySnapshotCommitted: %v", err)
	}
	if !committed {
		t.Fatal("unsupported entity result should still commit an empty chapter snapshot")
	}
}

func TestCompileEntitiesKeepsValidOccurrenceWhenSkippingUnsupportedOne(t *testing.T) {
	p, st := buildTestProject(t)
	scenes := seedEntityReverseIndex(t, p, st)
	seedSecondSceneCrewReverseIndex(t, p, st, scenes)

	fake := &fakeProvider{response: `{"entities":[{"canonical_name":"good-for-nothing crew","type":"group","occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["good-for-nothing crew"],"confidence":0.9},{"scene_id":"` + scenes[1].ID + `","surface_texts":["good-for-nothing crew"],"confidence":0.9}]}]}`}
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities with mixed occurrence support: %v", err)
	}
	if result.EntitiesBuilt != 1 {
		t.Fatalf("EntitiesBuilt = %d, want 1 entity with valid occurrence", result.EntitiesBuilt)
	}

	entities, occurrences, err := st.EntityCounts()
	if err != nil {
		t.Fatalf("EntityCounts: %v", err)
	}
	if entities != 1 || occurrences != 1 {
		t.Fatalf("entity counts = (%d, %d), want (1, 1)", entities, occurrences)
	}
	rows, err := st.EntityRowsForChapter("ch-0001")
	if err != nil {
		t.Fatalf("EntityRowsForChapter: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("entity rows = %d, want 1", len(rows))
	}
	occRows, err := st.OccurrencesForEntity(rows[0].ID, "ch-0001", 0)
	if err != nil {
		t.Fatalf("OccurrencesForEntity: %v", err)
	}
	if len(occRows) != 1 {
		t.Fatalf("occurrence rows = %d, want 1", len(occRows))
	}
	if occRows[0].SceneID != scenes[1].ID {
		t.Fatalf("occurrence scene = %s, want %s", occRows[0].SceneID, scenes[1].ID)
	}
	if len(occRows[0].SurfaceTexts) != 1 || occRows[0].SurfaceTexts[0] != "good-for-nothing crew" {
		t.Fatalf("occurrence surfaces = %#v, want good-for-nothing crew", occRows[0].SurfaceTexts)
	}
}

func TestCompileEntitiesSalvagesTruncatedJSON(t *testing.T) {
	p, st := buildTestProject(t)
	scenes := seedEntityReverseIndex(t, p, st)

	truncated := `{"entities":[` +
		`{"canonical_name":"Mara","type":"character","aliases":["Maraa"],"occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara"],"confidence":0.95}]},` +
		`{"canonical_name":"Petar","type":"character","occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Old Petar"]}`
	fake := &fakeProvider{response: truncated}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities with truncated JSON: %v", err)
	}
	if result.EntitiesBuilt != 1 {
		t.Fatalf("EntitiesBuilt = %d, want 1 salvaged entity", result.EntitiesBuilt)
	}

	entities, occurrences, err := st.EntityCounts()
	if err != nil {
		t.Fatalf("EntityCounts: %v", err)
	}
	if entities != 1 || occurrences != 1 {
		t.Fatalf("entity counts = (%d, %d), want (1, 1)", entities, occurrences)
	}
}

func TestCompileEntitiesEmptyTruncatedJSONDoesNotFail(t *testing.T) {
	p, st := buildTestProject(t)
	seedEntityReverseIndex(t, p, st)
	fake := &fakeProvider{response: `{"entities":[{"canonical_name":"Mara"`}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities with empty truncated JSON: %v", err)
	}
	if result.EntitiesBuilt != 0 {
		t.Fatalf("EntitiesBuilt = %d, want 0", result.EntitiesBuilt)
	}
}

func TestCompileEntitiesMalformedJSONFails(t *testing.T) {
	p, st := buildTestProject(t)
	seedEntityReverseIndex(t, p, st)
	fake := &fakeProvider{response: `{"entities":"not an array"}`}

	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err == nil {
		t.Fatal("expected malformed entity JSON to fail")
	}
	if !strings.Contains(err.Error(), "parse entity response for ch-0001") {
		t.Fatalf("error = %v, want parse entity response context", err)
	}
	committed, err := st.IsEntitySnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsEntitySnapshotCommitted: %v", err)
	}
	if committed {
		t.Fatal("malformed entity JSON should not commit an entity snapshot")
	}
	entities, occurrences, err := st.EntityCounts()
	if err != nil {
		t.Fatalf("EntityCounts: %v", err)
	}
	if entities != 0 || occurrences != 0 {
		t.Fatalf("entity counts = (%d, %d), want (0, 0)", entities, occurrences)
	}
}

func TestCompileEntitiesEmptyResultWritesCommittedSnapshot(t *testing.T) {
	p, st := buildTestProject(t)
	seedEntityReverseIndex(t, p, st)
	fake := &fakeProvider{response: `{"entities":[]}`}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities: %v", err)
	}
	if result.EntitiesBuilt != 0 {
		t.Fatalf("EntitiesBuilt = %d, want 0", result.EntitiesBuilt)
	}
	committed, err := st.IsEntitySnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsEntitySnapshotCommitted: %v", err)
	}
	if !committed {
		t.Fatal("empty entity result should still commit a chapter snapshot")
	}

	result, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("second compile entities: %v", err)
	}
	if result.EntitiesBuilt != 0 {
		t.Fatalf("second EntitiesBuilt = %d, want 0", result.EntitiesBuilt)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("provider calls = %d, want 1 after committed empty snapshot", len(fake.requests))
	}
}

func TestCompileWritesResponseAudit(t *testing.T) {
	p, st := buildTestProject(t)
	seedEntityReverseIndex(t, p, st)
	fake := &fakeProvider{
		response:     "",
		finishReason: "length",
		promptTokens: 101,
		outputTokens: 202,
	}

	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities with empty response: %v", err)
	}

	runDir := p.Path(filepath.Join(project.RunsDir, result.RunID))
	rawDir := filepath.Join(runDir, "raw-responses")
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		t.Fatalf("read raw responses: %v", err)
	}
	var metaPath string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".meta.json") {
			metaPath = filepath.Join(rawDir, entry.Name())
			break
		}
	}
	if metaPath == "" {
		t.Fatal("expected raw response metadata sidecar")
	}

	meta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read response metadata: %v", err)
	}
	for _, want := range []string{
		`"finish_reason": "length"`,
		`"prompt_tokens": 101`,
		`"output_tokens": 202`,
		`"content_bytes": 0`,
		`"content_empty": true`,
	} {
		if !strings.Contains(string(meta), want) {
			t.Fatalf("metadata missing %s:\n%s", want, meta)
		}
	}

	tasks, err := os.ReadFile(filepath.Join(runDir, "tasks.jsonl"))
	if err != nil {
		t.Fatalf("read tasks: %v", err)
	}
	for _, want := range []string{
		`"finish_reason":"length"`,
		`"prompt_tokens":101`,
		`"output_tokens":202`,
		`"content_bytes":0`,
		`"content_empty":true`,
		`"started_at":`,
		`"finished_at":`,
	} {
		if !strings.Contains(string(tasks), want) {
			t.Fatalf("tasks missing %s:\n%s", want, tasks)
		}
	}

	firstTaskLine := strings.Split(strings.TrimSpace(string(tasks)), "\n")[0]
	var task compiler.TaskRecord
	if err := json.Unmarshal([]byte(firstTaskLine), &task); err != nil {
		t.Fatalf("parse task record: %v", err)
	}
	if task.StartedAt == "" || task.FinishedAt == "" {
		t.Fatalf("task timing missing: %#v", task)
	}
	if task.Response == nil {
		t.Fatal("task response audit missing")
	}

	promptData, err := os.ReadFile(filepath.Join(runDir, "prompts", task.TaskID+".md"))
	if err != nil {
		t.Fatalf("read prompt transcript: %v", err)
	}
	for _, want := range []string{
		"# Compile Prompt",
		"Task ID: `" + task.TaskID + "`",
		"Model: `fake-model`",
		"Temperature: `0.1`",
		"JSON mode: `true`",
		"## system",
		"## user",
		"Consolidate candidate entities from scene-card reverse-index results",
	} {
		if !strings.Contains(string(promptData), want) {
			t.Fatalf("prompt transcript missing %s:\n%s", want, promptData)
		}
	}

	summaryData, err := os.ReadFile(filepath.Join(runDir, "summary.json"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	var summary map[string]any
	if err := json.Unmarshal(summaryData, &summary); err != nil {
		t.Fatalf("parse summary: %v", err)
	}
	for key, want := range map[string]float64{
		"total_tasks":                       1,
		"total_provider_calls":              1,
		"total_prompt_tokens":               101,
		"total_output_tokens":               202,
		"max_observed_provider_concurrency": 1,
	} {
		if got, _ := summary[key].(float64); got != want {
			t.Fatalf("summary[%s] = %v, want %v\n%s", key, summary[key], want, summaryData)
		}
	}
}

func TestCompileEntitiesUsesReverseIndexContext(t *testing.T) {
	p, st := buildTestProject(t)
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerScenes})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}

	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	scenes, err := st.AllScenes()
	if err != nil {
		t.Fatalf("AllScenes: %v", err)
	}
	if len(paragraphs) == 0 || len(scenes) == 0 {
		t.Fatal("expected paragraphs and scenes")
	}

	summaries := `{"record_type":"chapter_summary","chapter_id":"ch-0001","summary":"Mara receives the bell warning [` + paragraphs[0].ID + `].","themes":["warnings"],"unresolved":["Who wrote the warning?"],"evidence":["` + paragraphs[0].ID + `"],"generation":{},"status":"generated"}` + "\n" +
		`{"record_type":"book_summary","summary":"A lakeside mystery turns on bells and memory.","themes":["memory"],"evidence":["ch-0001"],"generation":{},"status":"generated"}` + "\n"
	if err := os.WriteFile(p.Path(filepath.Join(project.ModelDir, "summaries.jsonl")), []byte(summaries), 0o644); err != nil {
		t.Fatalf("write summaries: %v", err)
	}

	rawCard := `{"record_type":"scene_card","scene_id":"` + scenes[0].ID + `","title":"Bell warning","summary":"Mara meets Old Petar near the lake.","entities":["Maraa"],"pov":["Mara"],"participants":["Mara","Old Petar"],"locations":["Lake edge"],"unresolved":["Why the bell must stay silent"],"evidence":["` + paragraphs[0].ID + `"],"generation":{},"status":"verified"}`
	if err := st.InsertSceneCard(store.SceneCardRow{
		SceneID: scenes[0].ID,
		Title:   "Bell warning",
		Summary: "Mara meets Old Petar near the lake.",
		Evidence: []string{
			paragraphs[0].ID,
		},
		Status:  "verified",
		RawJSON: rawCard,
	}); err != nil {
		t.Fatalf("InsertSceneCard: %v", err)
	}

	if err := st.RebuildReverseIndex(); err != nil {
		t.Fatalf("RebuildReverseIndex: %v", err)
	}

	fake := &fakeProvider{response: `{"entities":[{"canonical_name":"Mara","type":"character","aliases":["Maraa"],"occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara","Maraa"],"confidence":0.95,"flags":[{"type":"possible_typo","value":"Maraa","suggested":"Mara","confidence":0.8}]}]}]}`}
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities: %v", err)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("Generate calls = %d, want 1", len(fake.requests))
	}

	prompt := fake.requests[0].Messages[1].Content
	for _, want := range []string{
		"Consolidate candidate entities from scene-card reverse-index results",
		"copy surface_texts exactly from that occurrence's own scene block",
		"Reverse-index candidates:",
		"- scene " + scenes[0].ID,
		"entities: Maraa",
		"locations: Lake edge",
		"participants: Mara; Old Petar",
		"pov: Mara",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("entity prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{
		"Existing extraction context",
		"Book summary:",
		"Chapter summary:",
		"Authoritative manuscript paragraph excerpts:",
		paragraphs[0].Text,
	} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("entity prompt unexpectedly contains %q:\n%s", unwanted, prompt)
		}
	}
}

func TestCompileVerificationWithFakeProvider(t *testing.T) {
	p, st := buildTestProject(t)
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{Layer: compiler.LayerScenes})
	if err != nil {
		t.Fatalf("compile scenes: %v", err)
	}
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	fakeCards := &fakeProvider{responses: []string{
		`{"title":"Mara walks","summary":"Mara walks the road.","evidence":["` + paragraphs[0].ID + `"]}`,
		`{"title":"Sunrise","summary":"The sun rises over the hills.","evidence":["` + paragraphs[1].ID + `"]}`,
	}}
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSceneCards,
		ExtractionProvider: fakeCards,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile scene-cards: %v", err)
	}

	fakeVerify := &fakeProvider{response: `{"supported":true,"support_level":"explicit","epistemic_type":"scene_card","overstatement":null,"missing_counterevidence":false}`}
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:                compiler.LayerVerification,
		VerificationProvider: fakeVerify,
		VerificationModel:    "verify-model",
	})
	if err != nil {
		t.Fatalf("compile verification: %v", err)
	}
	if result.VerificationsBuilt != 2 {
		t.Fatalf("VerificationsBuilt = %d, want 2", result.VerificationsBuilt)
	}
	assertRunPendingFiles(t, p, result.RunID, compiler.LayerVerification, 2)
	assertRunCommitLogSequences(t, p, result.RunID, compiler.LayerVerification, []int{0, 1})

	cards, err := st.AllSceneCards()
	if err != nil {
		t.Fatalf("AllSceneCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("cards = %d, want 2", len(cards))
	}
	for _, card := range cards {
		if card.Status != "verified" {
			t.Fatalf("card %s status = %q, want verified", card.SceneID, card.Status)
		}
		if !strings.Contains(card.RawJSON, `"verification"`) {
			t.Fatalf("card %s raw json missing verification: %s", card.SceneID, card.RawJSON)
		}
	}
}
