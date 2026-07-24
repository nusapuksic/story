package compiler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/store"
)

// Layer names used in --layer flag and run records.
const (
	LayerScenes     = "scenes"
	LayerSceneCards = "scene-cards"
	LayerSummaries  = "summaries"
)

// Options controls a compilation run.
type Options struct {
	// Layer restricts compilation to a single layer.
	// Empty string means "all implemented layers".
	Layer string
	// ChapterID restricts compilation to one chapter.
	ChapterID string
	// Force causes already-generated records to be recomputed.
	Force bool
	// ExtractionProvider is the LLM provider for extraction tasks.
	// May be nil for explicit-only scene detection.
	ExtractionProvider provider.Provider
	// ExtractionModel is the model to use for extraction calls.
	ExtractionModel string
}

// Result summarizes a completed compilation run.
type Result struct {
	RunID          string
	ScenesBuilt    int
	CardsBuilt     int
	SummariesBuilt int
}

// Compile runs the compilation pipeline for the given project.  It opens and
// closes the SQLite index.
func Compile(ctx context.Context, p *project.Project, st *store.Store, opts Options) (Result, error) {
	ctx = contextOrBackground(ctx)

	if opts.Layer != "" && opts.Layer != LayerScenes && opts.Layer != LayerSceneCards && opts.Layer != LayerSummaries {
		return Result{}, fmt.Errorf("unknown layer %q; supported: %s, %s, %s",
			opts.Layer, LayerScenes, LayerSceneCards, LayerSummaries)
	}

	cfg := sceneDetectConfig{
		Mode:                p.Config.Compile.SceneDetection,
		TargetContextTokens: p.Config.Compile.TargetContextTokens,
		MaxOutputTokens:     p.Config.Compile.MaximumOutputTokens,
		OverlapParagraphs:   p.Config.Compile.WindowOverlapParagraphs,
		Temperature:         p.Config.Compile.Temperature,
	}
	if cfg.TargetContextTokens <= 0 {
		cfg.TargetContextTokens = 12000
	}

	run, err := newRun(p, "compile", opts.Layer, opts.ChapterID)
	if err != nil {
		return Result{}, err
	}

	scenesBuilt, cardsBuilt, summariesBuilt, compileErr := runLayers(ctx, p, st, opts, cfg, run)
	if compileErr != nil {
		_ = run.fail(compileErr)
		return Result{RunID: run.Record.RunID}, compileErr
	}
	if err := run.complete(); err != nil {
		return Result{RunID: run.Record.RunID}, err
	}
	_ = run.saveSummary(scenesBuilt, cardsBuilt, summariesBuilt)
	return Result{
		RunID:          run.Record.RunID,
		ScenesBuilt:    scenesBuilt,
		CardsBuilt:     cardsBuilt,
		SummariesBuilt: summariesBuilt,
	}, nil
}

// runLayers executes the requested compilation layers.
func runLayers(
	ctx context.Context,
	p *project.Project,
	st *store.Store,
	opts Options,
	cfg sceneDetectConfig,
	run *Run,
) (scenesBuilt, cardsBuilt, summariesBuilt int, err error) {
	// Determine which chapters to process.
	chapters, err := chaptersToProcess(st, opts.ChapterID)
	if err != nil {
		return 0, 0, 0, err
	}
	if len(chapters) == 0 {
		return 0, 0, 0, nil
	}

	// Scenes layer (Layer 2).
	if opts.Layer == "" || opts.Layer == LayerScenes {
		n, err := compileScenes(ctx, p, st, chapters, opts, cfg, run)
		if err != nil {
			return 0, 0, 0, err
		}
		scenesBuilt = n
	}

	// Scene-cards layer (Layer 3).
	if opts.Layer == "" || opts.Layer == LayerSceneCards {
		if opts.ExtractionProvider == nil {
			return scenesBuilt, 0, 0, errors.New(
				"no LLM provider configured: scene cards require an extraction provider; " +
					"configure [llm] in story.toml")
		}
		n, err := compileSceneCards(ctx, p, st, chapters, opts, cfg, run)
		if err != nil {
			return scenesBuilt, 0, 0, err
		}
		cardsBuilt = n
	}

	// Summaries layer (Layer 6 MVP).
	if opts.Layer == "" || opts.Layer == LayerSummaries {
		if opts.ExtractionProvider == nil {
			return scenesBuilt, cardsBuilt, 0, errors.New(
				"no LLM provider configured: summaries require an extraction provider; " +
					"configure [llm] in story.toml")
		}
		n, err := compileSummaries(ctx, p, st, chapters, opts, cfg, run)
		if err != nil {
			return scenesBuilt, cardsBuilt, 0, err
		}
		summariesBuilt = n
	}

	return scenesBuilt, cardsBuilt, summariesBuilt, nil
}

// chaptersToProcess returns the chapters to compile, optionally filtered to one.
func chaptersToProcess(st *store.Store, chapterID string) ([]store.ChapterRow, error) {
	if chapterID != "" {
		ch, err := st.InspectChapter(chapterID)
		if err != nil {
			return nil, err
		}
		return []store.ChapterRow{ch}, nil
	}
	return st.AllChapters()
}

// compileScenes runs scene boundary detection for all requested chapters and
// writes scenes to the store and JSONL file.
func compileScenes(
	ctx context.Context,
	p *project.Project,
	st *store.Store,
	chapters []store.ChapterRow,
	opts Options,
	cfg sceneDetectConfig,
	run *Run,
) (int, error) {
	scenesFile, err := openAppendJSONL(p.Path(filepath.Join(project.ModelDir, "scenes.jsonl")))
	if err != nil {
		return 0, err
	}
	defer scenesFile.Close()

	total := 0
	for _, ch := range chapters {
		if opts.Force {
			// --force: delete existing scenes (including snapshot marker) and recompute.
			if err := st.DeleteScenesForChapter(ch.ID); err != nil {
				return total, err
			}
		} else {
			committed, err := st.IsChapterSnapshotCommitted(ch.ID)
			if err != nil {
				return total, err
			}
			if committed {
				// A complete, validated snapshot already exists; skip this chapter.
				continue
			}
			// No committed snapshot: a previous run may have left partial scenes.
			// Discard them so detection starts fresh.
			if err := st.DeleteScenesForChapter(ch.ID); err != nil {
				return total, err
			}
		}

		paragraphs, err := st.ParagraphsByChapter(ch.ID)
		if err != nil {
			return total, err
		}

		breakOrdinals, err := st.SceneBreakOrdinals(ch.ID)
		if err != nil {
			return total, err
		}

		scenes, err := detectScenes(ctx, ch, paragraphs, nil, breakOrdinals,
			opts.ExtractionProvider, opts.ExtractionModel, cfg, run)
		if err != nil {
			return total, fmt.Errorf("detect scenes for chapter %s: %w", ch.ID, err)
		}

		// Validate the detected scenes form a complete partition before committing.
		if err := ValidateScenePartition(paragraphs, scenes); err != nil {
			return total, fmt.Errorf("scene partition invalid for chapter %s: %w", ch.ID, err)
		}

		for _, sc := range scenes {
			row := store.SceneRow{
				ID:             sc.ID,
				ChapterID:      sc.ChapterID,
				ParagraphStart: sc.ParagraphStart,
				ParagraphEnd:   sc.ParagraphEnd,
				Ordinal:        sc.Ordinal,
				BoundarySource: sc.BoundarySource,
				Status:         sc.Status,
			}
			if err := st.InsertScene(row); err != nil {
				return total, err
			}
			if err := appendJSONL(scenesFile, sc); err != nil {
				return total, err
			}
			total++
		}

		// Explicitly commit the snapshot: append a chapter_snapshot record to the
		// JSONL and mark the chapter as committed in the store.  Both writes must
		// succeed for the snapshot to be considered complete.
		committedAt := time.Now().UTC().Format(time.RFC3339)
		snap := ChapterSnapshotRecord{
			RecordType:  "chapter_snapshot",
			ChapterID:   ch.ID,
			SceneCount:  len(scenes),
			CommittedAt: committedAt,
		}
		if err := appendJSONL(scenesFile, snap); err != nil {
			return total, fmt.Errorf("write chapter_snapshot for %s: %w", ch.ID, err)
		}
		if err := st.MarkChapterSnapshotCommitted(ch.ID, committedAt); err != nil {
			return total, err
		}
	}
	return total, nil
}

// compileSceneCards runs scene card extraction for all scenes in the requested
// chapters.
func compileSceneCards(
	ctx context.Context,
	p *project.Project,
	st *store.Store,
	chapters []store.ChapterRow,
	opts Options,
	cfg sceneDetectConfig,
	run *Run,
) (int, error) {
	scenesFile, err := openAppendJSONL(p.Path(filepath.Join(project.ModelDir, "scenes.jsonl")))
	if err != nil {
		return 0, err
	}
	defer scenesFile.Close()

	total := 0
	for _, ch := range chapters {
		scenes, err := st.ScenesByChapter(ch.ID)
		if err != nil {
			return total, err
		}
		if len(scenes) == 0 {
			return total, fmt.Errorf("no scenes found for chapter %s; run 'story compile --layer scenes' first", ch.ID)
		}

		paragraphs, err := st.ParagraphsByChapter(ch.ID)
		if err != nil {
			return total, err
		}
		paraByID := make(map[string]store.ParagraphRow, len(paragraphs))
		for _, pp := range paragraphs {
			paraByID[pp.ID] = pp
		}

		for _, sc := range scenes {
			if !opts.Force {
				if _, err := st.InspectSceneCard(sc.ID); err == nil {
					// Already extracted.
					continue
				}
			}

			sceneParagraphs := paragraphsInScene(paragraphs, paraByID, sc)
			card, err := extractSceneCard(ctx, sc, sceneParagraphs,
				opts.ExtractionProvider, opts.ExtractionModel, cfg, run)
			if err != nil {
				return total, fmt.Errorf("extract scene card for %s: %w", sc.ID, err)
			}

			row := store.SceneCardRow{
				SceneID:         card.SceneID,
				Title:           card.Title,
				Summary:         card.Summary,
				Evidence:        card.Evidence,
				GenerationRun:   card.Generation.RunID,
				GenerationModel: card.Generation.Model,
				PromptVersion:   card.Generation.PromptVersion,
				Status:          card.Status,
			}
			rawBytes, _ := json.Marshal(card)
			row.RawJSON = string(rawBytes)

			if err := st.InsertSceneCard(row); err != nil {
				return total, err
			}
			if err := appendJSONL(scenesFile, card); err != nil {
				return total, err
			}
			total++
		}
	}
	return total, nil
}

// paragraphsInScene returns the ordered subset of paragraphs belonging to a
// scene (inclusive of start and end).
func paragraphsInScene(
	ordered []store.ParagraphRow,
	byID map[string]store.ParagraphRow,
	scene store.SceneRow,
) []store.ParagraphRow {
	// Find start and end indices.
	startOrd := -1
	endOrd := -1
	for _, p := range ordered {
		if p.ID == scene.ParagraphStart {
			startOrd = p.Ordinal
		}
		if p.ID == scene.ParagraphEnd {
			endOrd = p.Ordinal
		}
	}
	if startOrd < 0 || endOrd < 0 {
		return nil
	}
	var out []store.ParagraphRow
	for _, p := range ordered {
		if p.Ordinal >= startOrd && p.Ordinal <= endOrd {
			out = append(out, p)
		}
	}
	return out
}

// openAppendJSONL opens a JSONL file for appending, creating it if needed.
func openAppendJSONL(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

// appendJSONL encodes v as a single JSON line into w.
func appendJSONL(w *os.File, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// ReadScenesJSONL reads all scene and scene card records from model/scenes.jsonl.
func ReadScenesJSONL(path string) ([]SceneRecord, []SceneCardRecord, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var scenes []SceneRecord
	var cards []SceneCardRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var typed struct {
			RecordType string `json:"record_type"`
		}
		if err := json.Unmarshal(line, &typed); err != nil {
			continue
		}
		switch typed.RecordType {
		case "scene":
			var r SceneRecord
			if err := json.Unmarshal(line, &r); err == nil {
				scenes = append(scenes, r)
			}
		case "scene_card":
			var r SceneCardRecord
			if err := json.Unmarshal(line, &r); err == nil {
				cards = append(cards, r)
			}
		}
	}
	return scenes, cards, sc.Err()
}
