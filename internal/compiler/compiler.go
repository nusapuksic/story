package compiler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nusapuksic/story/internal/config"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/store"
)

// Layer names used in --layer flag and run records.
const (
	LayerScenes       = "scenes"
	LayerSceneCards   = "scene-cards"
	LayerEntities     = "entities"
	LayerVerification = "verification"
	LayerSummaries    = "summaries"
)

const minParagraphsForSingleSceneSplitSuggestion = 6

// Options controls a compilation run.
type Options struct {
	// Layer restricts compilation to a single layer.
	// Empty string means "all implemented layers".
	Layer string
	// ChapterID restricts compilation to one chapter.
	ChapterID string
	// Force causes already-generated records to be recomputed.
	Force bool
	// SceneCardFailurePolicy controls recovery from invalid scene-card model output.
	// Empty string uses [compile].scene_card_failure_policy, defaulting to retry-fallback.
	SceneCardFailurePolicy string
	// VerificationMode controls verification during ordinary full compile.
	// Empty string uses [compile].verification_mode or legacy [compile].verification.
	VerificationMode string
	// Progress receives optional compile progress events. Nil disables reporting.
	Progress ProgressFunc
	// ExtractionProvider is the LLM provider for extraction tasks.
	// May be nil for explicit-only scene detection.
	ExtractionProvider provider.Provider
	// ExtractionModel is the model to use for extraction calls.
	ExtractionModel string
	// VerificationProvider is the LLM provider for verification tasks.
	VerificationProvider provider.Provider
	// VerificationModel is the model to use for verification calls.
	VerificationModel string
}

// ProgressFunc receives structured progress updates during compile.
type ProgressFunc func(ProgressEvent)

// ProgressEvent describes a human-readable compile progress update.
type ProgressEvent struct {
	Layer     string `json:"layer,omitempty"`
	Stage     string `json:"stage,omitempty"`
	ChapterID string `json:"chapter_id,omitempty"`
	SceneID   string `json:"scene_id,omitempty"`
	Current   int    `json:"current,omitempty"`
	Total     int    `json:"total,omitempty"`
	Message   string `json:"message,omitempty"`
}

func reportProgress(opts Options, event ProgressEvent) {
	emitProgress(opts.Progress, event)
}

func emitProgress(progress ProgressFunc, event ProgressEvent) {
	if progress != nil {
		progress(event)
	}
}

// SceneCardRecoveryEvent identifies a scene card that needed extraction recovery.
type SceneCardRecoveryEvent struct {
	SceneID   string `json:"scene_id"`
	ChapterID string `json:"chapter_id"`
	Action    string `json:"action"`
	Attempts  int    `json:"attempts"`
	Reason    string `json:"reason,omitempty"`
}

// Result summarizes a completed compilation run.
type Result struct {
	RunID                   string
	ScenesBuilt             int
	CardsBuilt              int
	SceneCardRecoveries     int
	SceneCardRecoveryEvents []SceneCardRecoveryEvent
	EntitiesBuilt           int
	VerificationsBuilt      int
	SummariesBuilt          int
}

// Compile runs the compilation pipeline for the given project.  It opens and
// closes the SQLite index.
func Compile(ctx context.Context, p *project.Project, st *store.Store, opts Options) (Result, error) {
	ctx = contextOrBackground(ctx)

	if !isSupportedLayer(opts.Layer) {
		return Result{}, fmt.Errorf("unknown layer %q; supported: %s, %s, %s, %s, %s",
			opts.Layer, LayerScenes, LayerSceneCards, LayerEntities, LayerVerification, LayerSummaries)
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

	verificationMode, err := EffectiveVerificationMode(opts, p.Config.Compile)
	if err != nil {
		return Result{}, err
	}
	opts.VerificationMode = verificationMode

	run, err := newRun(p, "compile", opts.Layer, opts.ChapterID)
	if err != nil {
		return Result{}, err
	}

	scenesBuilt, cardsBuilt, sceneCardRecoveryEvents, entitiesBuilt, verificationsBuilt, summariesBuilt, compileErr := runLayers(ctx, p, st, opts, cfg, run)
	if compileErr != nil {
		_ = run.fail(compileErr)
		return Result{RunID: run.id()}, compileErr
	}
	if err := run.complete(); err != nil {
		return Result{RunID: run.id()}, err
	}
	_ = run.saveSummary(scenesBuilt, cardsBuilt, len(sceneCardRecoveryEvents), sceneCardRecoveryEvents, entitiesBuilt, verificationsBuilt, summariesBuilt)
	return Result{
		RunID:                   run.id(),
		ScenesBuilt:             scenesBuilt,
		CardsBuilt:              cardsBuilt,
		SceneCardRecoveries:     len(sceneCardRecoveryEvents),
		SceneCardRecoveryEvents: sceneCardRecoveryEvents,
		EntitiesBuilt:           entitiesBuilt,
		VerificationsBuilt:      verificationsBuilt,
		SummariesBuilt:          summariesBuilt,
	}, nil
}

func isSupportedLayer(layer string) bool {
	switch layer {
	case "", LayerScenes, LayerSceneCards, LayerEntities, LayerVerification, LayerSummaries:
		return true
	default:
		return false
	}
}

// Verification modes used by [compile].verification_mode.
const (
	VerificationModeOff       = "off"
	VerificationModeRecovered = "recovered"
	VerificationModeSelective = "selective"
	VerificationModeAll       = "all"
)

// EffectiveVerificationMode normalizes the verification mode for one compile run.
func EffectiveVerificationMode(opts Options, cfg config.CompileConfig) (string, error) {
	if strings.TrimSpace(opts.VerificationMode) != "" {
		return normalizeVerificationMode(opts.VerificationMode)
	}
	if strings.TrimSpace(cfg.VerificationMode) != "" {
		return normalizeVerificationMode(cfg.VerificationMode)
	}
	if cfg.Verification {
		return VerificationModeAll, nil
	}
	return VerificationModeOff, nil
}

func normalizeVerificationMode(mode string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(mode))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "", VerificationModeAll:
		return VerificationModeAll, nil
	case VerificationModeOff, VerificationModeRecovered, VerificationModeSelective:
		return normalized, nil
	default:
		return "", fmt.Errorf("unknown verification mode %q; supported: %s, %s, %s, %s",
			mode, VerificationModeOff, VerificationModeRecovered, VerificationModeSelective, VerificationModeAll)
	}
}

func shouldRunVerificationLayer(layer, verificationMode string) bool {
	if layer == LayerVerification {
		return true
	}
	return layer == "" && verificationMode != VerificationModeOff
}

// runLayers executes the requested compilation layers.
func runLayers(
	ctx context.Context,
	p *project.Project,
	st *store.Store,
	opts Options,
	cfg sceneDetectConfig,
	run *Run,
) (scenesBuilt, cardsBuilt int, sceneCardRecoveryEvents []SceneCardRecoveryEvent, entitiesBuilt, verificationsBuilt, summariesBuilt int, err error) {
	// Determine which chapters to process.
	chapters, err := chaptersToProcess(st, opts.ChapterID)
	if err != nil {
		return 0, 0, nil, 0, 0, 0, err
	}
	if len(chapters) == 0 {
		return 0, 0, nil, 0, 0, 0, nil
	}
	reportProgress(opts, ProgressEvent{
		Stage:   "run-start",
		Total:   len(chapters),
		Message: fmt.Sprintf("Compile: %d chapter(s) selected", len(chapters)),
	})

	// Scenes layer (Layer 2).
	if opts.Layer == "" || opts.Layer == LayerScenes {
		reportProgress(opts, ProgressEvent{Layer: LayerScenes, Stage: "layer-start", Total: len(chapters), Message: "Scenes: starting"})
		n, err := compileScenes(ctx, p, st, chapters, opts, cfg, run)
		if err != nil {
			return 0, 0, nil, 0, 0, 0, err
		}
		scenesBuilt = n
		reportProgress(opts, ProgressEvent{Layer: LayerScenes, Stage: "layer-complete", Current: n, Message: fmt.Sprintf("Scenes: completed (%d built)", n)})
	}

	// Scene-cards layer (Layer 3).
	if opts.Layer == "" || opts.Layer == LayerSceneCards {
		reportProgress(opts, ProgressEvent{Layer: LayerSceneCards, Stage: "layer-start", Total: len(chapters), Message: "Scene cards: starting"})
		if opts.ExtractionProvider == nil {
			return scenesBuilt, 0, nil, 0, 0, 0, errors.New(
				"no LLM provider configured: scene cards require an extraction provider; " +
					"configure [llm] in story.toml")
		}
		n, recoveryEvents, err := compileSceneCards(ctx, p, st, chapters, opts, cfg, run)
		if err != nil {
			return scenesBuilt, 0, nil, 0, 0, 0, err
		}
		cardsBuilt = n
		sceneCardRecoveryEvents = recoveryEvents
		reportProgress(opts, ProgressEvent{Layer: LayerSceneCards, Stage: "layer-complete", Current: n, Message: fmt.Sprintf("Scene cards: completed (%d built, %d recovered)", n, len(recoveryEvents))})
	}

	// Verification layer verifies generated factual records when enabled.
	if shouldRunVerificationLayer(opts.Layer, opts.VerificationMode) {
		reportProgress(opts, ProgressEvent{Layer: LayerVerification, Stage: "layer-start", Total: len(chapters), Message: "Verification: starting"})
		if opts.VerificationProvider == nil {
			return scenesBuilt, cardsBuilt, sceneCardRecoveryEvents, entitiesBuilt, 0, 0, errors.New(
				"no LLM provider configured: verification requires a verification provider; " +
					"configure [llm.roles.verification] in story.toml")
		}
		n, err := compileVerification(ctx, p, st, chapters, opts, cfg, run)
		if err != nil {
			return scenesBuilt, cardsBuilt, sceneCardRecoveryEvents, entitiesBuilt, 0, 0, err
		}
		verificationsBuilt = n
		reportProgress(opts, ProgressEvent{Layer: LayerVerification, Stage: "layer-complete", Current: n, Message: fmt.Sprintf("Verification: completed (%d built)", n)})
	}

	if err := rebuildReverseIndexForCompile(st, opts); err != nil {
		return scenesBuilt, cardsBuilt, sceneCardRecoveryEvents, entitiesBuilt, verificationsBuilt, summariesBuilt, err
	}

	// Summaries layer.
	if opts.Layer == "" || opts.Layer == LayerSummaries {
		reportProgress(opts, ProgressEvent{Layer: LayerSummaries, Stage: "layer-start", Total: len(chapters), Message: "Summaries: starting"})
		if opts.ExtractionProvider == nil {
			return scenesBuilt, cardsBuilt, sceneCardRecoveryEvents, entitiesBuilt, verificationsBuilt, 0, errors.New(
				"no LLM provider configured: summaries require an extraction provider; " +
					"configure [llm] in story.toml")
		}
		n, err := compileSummaries(ctx, p, st, chapters, opts, cfg, run)
		if err != nil {
			return scenesBuilt, cardsBuilt, sceneCardRecoveryEvents, entitiesBuilt, verificationsBuilt, 0, err
		}
		summariesBuilt = n
		reportProgress(opts, ProgressEvent{Layer: LayerSummaries, Stage: "layer-complete", Current: n, Message: fmt.Sprintf("Summaries: completed (%d built)", n)})
	}

	// Entities layer.
	if opts.Layer == "" || opts.Layer == LayerEntities {
		reportProgress(opts, ProgressEvent{Layer: LayerEntities, Stage: "layer-start", Total: len(chapters), Message: "Entities: starting"})
		n, err := compileEntities(ctx, p, st, chapters, opts, cfg, run)
		if err != nil {
			return scenesBuilt, cardsBuilt, sceneCardRecoveryEvents, 0, verificationsBuilt, summariesBuilt, err
		}
		entitiesBuilt = n
		reportProgress(opts, ProgressEvent{Layer: LayerEntities, Stage: "layer-complete", Current: n, Message: fmt.Sprintf("Entities: completed (%d built)", n)})
	}

	return scenesBuilt, cardsBuilt, sceneCardRecoveryEvents, entitiesBuilt, verificationsBuilt, summariesBuilt, nil
}

func rebuildReverseIndexForCompile(st *store.Store, opts Options) error {
	reportProgress(opts, ProgressEvent{Layer: "reverse-index", Stage: "item-start", Message: "Reverse index: rebuilding"})
	if err := st.RebuildReverseIndex(); err != nil {
		return err
	}
	reportProgress(opts, ProgressEvent{Layer: "reverse-index", Stage: "item-complete", Message: "Reverse index: rebuilt"})
	return nil
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
	staging, err := optionalRunStagingStore(run)
	if err != nil {
		return 0, err
	}
	scenesFile, err := openAppendJSONL(p.Path(filepath.Join(project.ModelDir, "scenes.jsonl")))
	if err != nil {
		return 0, err
	}
	defer scenesFile.Close()
	committer := compileArtifactCommitter{st: st, staging: staging, scenesFile: scenesFile}

	items := make([]OrderedWorkItem[sceneWorkInput], 0, len(chapters))
	for chapterIndex, ch := range chapters {
		reportProgress(opts, ProgressEvent{Layer: LayerScenes, Stage: "item-start", ChapterID: ch.ID, Current: chapterIndex + 1, Total: len(chapters), Message: fmt.Sprintf("Scenes %s (%d/%d): preparing", ch.ID, chapterIndex+1, len(chapters))})
		if !opts.Force {
			committed, err := st.IsChapterSnapshotCommitted(ch.ID)
			if err != nil {
				return 0, err
			}
			if committed {
				// A complete, validated snapshot already exists; skip this chapter.
				reportProgress(opts, ProgressEvent{Layer: LayerScenes, Stage: "item-skip", ChapterID: ch.ID, Current: chapterIndex + 1, Total: len(chapters), Message: fmt.Sprintf("Scenes %s (%d/%d): already current", ch.ID, chapterIndex+1, len(chapters))})
				continue
			}
		}

		paragraphs, err := st.ParagraphsByChapter(ch.ID)
		if err != nil {
			return 0, err
		}
		breakOrdinals, err := st.SceneBreakOrdinals(ch.ID)
		if err != nil {
			return 0, err
		}
		items = append(items, OrderedWorkItem[sceneWorkInput]{
			Sequence: len(items),
			TaskID:   ch.ID,
			Input: sceneWorkInput{
				Chapter:       ch,
				ChapterIndex:  chapterIndex,
				ChapterTotal:  len(chapters),
				Paragraphs:    paragraphs,
				BreakOrdinals: breakOrdinals,
			},
		})
	}

	total := 0
	err = RunOrderedWork(ctx, items, OrderedExecutorOptions{WorkerLimit: sceneDetectionWorkerLimit(cfg, opts)}, func(ctx context.Context, item OrderedWorkItem[sceneWorkInput]) (sceneWorkOutput, error) {
		input := item.Input
		scenes, err := detectScenes(ctx, p, input.Chapter, input.Paragraphs, nil, input.BreakOrdinals,
			opts.ExtractionProvider, opts.ExtractionModel, cfg, run)
		if err != nil {
			return sceneWorkOutput{}, fmt.Errorf("detect scenes for chapter %s: %w", input.Chapter.ID, err)
		}
		// Validate the detected scenes form a complete partition before committing.
		if err := ValidateScenePartition(input.Paragraphs, scenes); err != nil {
			return sceneWorkOutput{}, fmt.Errorf("scene partition invalid for chapter %s: %w", input.Chapter.ID, err)
		}
		output := sceneWorkOutput{
			Input:  input,
			Scenes: scenes,
			Snapshot: ChapterSnapshotRecord{
				RecordType:  "chapter_snapshot",
				ChapterID:   input.Chapter.ID,
				SceneCount:  len(scenes),
				CommittedAt: time.Now().UTC().Format(time.RFC3339),
			},
		}
		if staging != nil {
			ref, err := stageScenesWorkResult(staging, item.Sequence, output)
			if err != nil {
				return sceneWorkOutput{}, err
			}
			output.Staged = ref
		}
		return output, nil
	}, func(ctx context.Context, result OrderedWorkResult[sceneWorkOutput]) error {
		output := result.Output
		input := output.Input
		reportProgress(opts, ProgressEvent{Layer: LayerScenes, Stage: "item-running", ChapterID: input.Chapter.ID, Current: input.ChapterIndex + 1, Total: input.ChapterTotal, Message: fmt.Sprintf("Scenes %s (%d/%d): detecting boundaries across %d paragraph(s)", input.Chapter.ID, input.ChapterIndex+1, input.ChapterTotal, len(input.Paragraphs))})

		if err := committer.CommitScenes(output); err != nil {
			return err
		}
		total += len(output.Scenes)
		reportProgress(opts, ProgressEvent{Layer: LayerScenes, Stage: "item-complete", ChapterID: input.Chapter.ID, Current: input.ChapterIndex + 1, Total: input.ChapterTotal, Message: fmt.Sprintf("Scenes %s (%d/%d): built %d scene(s)", input.Chapter.ID, input.ChapterIndex+1, input.ChapterTotal, len(output.Scenes))})
		if shouldSuggestSingleSceneChapterSplit(output.Scenes, input.Paragraphs) {
			reportProgress(opts, ProgressEvent{Layer: LayerScenes, Stage: "suggestion", ChapterID: input.Chapter.ID, Current: input.ChapterIndex + 1, Total: input.ChapterTotal, Message: singleSceneChapterSplitSuggestion(input.Chapter.ID, input.Paragraphs)})
		}
		return nil
	})
	if err != nil {
		return total, err
	}
	return total, nil
}

func sceneDetectionWorkerLimit(cfg sceneDetectConfig, opts Options) int {
	if strings.EqualFold(strings.TrimSpace(cfg.Mode), "explicit") || opts.ExtractionProvider == nil {
		if n := runtime.NumCPU(); n > 0 {
			return n
		}
	}
	return 1
}
func sceneRowFromRecord(sc SceneRecord) store.SceneRow {
	return store.SceneRow{
		ID:             sc.ID,
		ChapterID:      sc.ChapterID,
		ParagraphStart: sc.ParagraphStart,
		ParagraphEnd:   sc.ParagraphEnd,
		Ordinal:        sc.Ordinal,
		BoundarySource: sc.BoundarySource,
		Status:         sc.Status,
	}
}

func stageScenesWorkResult(staging *RunStagingStore, sequence int, output sceneWorkOutput) (StagedResultRef, error) {
	if staging == nil {
		return StagedResultRef{}, nil
	}
	return staging.StageJSON(LayerScenes, StagedResultMeta{
		Sequence:      sequence,
		TaskID:        output.Input.Chapter.ID,
		TargetID:      output.Input.Chapter.ID,
		SchemaVersion: 1,
	}, stagedScenesPayload{
		Scenes:   output.Scenes,
		Snapshot: output.Snapshot,
	})
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
) (int, []SceneCardRecoveryEvent, error) {
	policy, err := sceneCardFailurePolicy(p, opts)
	if err != nil {
		return 0, nil, err
	}
	staging, err := optionalRunStagingStore(run)
	if err != nil {
		return 0, nil, err
	}

	scenesFile, err := openAppendJSONL(p.Path(filepath.Join(project.ModelDir, "scenes.jsonl")))
	if err != nil {
		return 0, nil, err
	}
	defer scenesFile.Close()
	committer := compileArtifactCommitter{st: st, staging: staging, scenesFile: scenesFile}

	total := 0
	recoveries := []SceneCardRecoveryEvent{}
	sequence := 0
	for chapterIndex, ch := range chapters {
		scenes, err := st.ScenesByChapter(ch.ID)
		if err != nil {
			return total, recoveries, err
		}
		if len(scenes) == 0 {
			return total, recoveries, fmt.Errorf("no scenes found for chapter %s; run 'story compile --layer scenes' first", ch.ID)
		}
		reportProgress(opts, ProgressEvent{Layer: LayerSceneCards, Stage: "chapter-start", ChapterID: ch.ID, Current: chapterIndex + 1, Total: len(chapters), Message: fmt.Sprintf("Scene cards %s (%d/%d): processing %d scene(s)", ch.ID, chapterIndex+1, len(chapters), len(scenes))})

		paragraphs, err := st.ParagraphsByChapter(ch.ID)
		if err != nil {
			return total, recoveries, err
		}
		paraByID := make(map[string]store.ParagraphRow, len(paragraphs))
		for _, pp := range paragraphs {
			paraByID[pp.ID] = pp
		}

		items := make([]OrderedWorkItem[sceneCardWorkInput], 0, len(scenes))
		for sceneIndex, sc := range scenes {
			if !opts.Force {
				if _, err := st.InspectSceneCard(sc.ID); err == nil {
					// Already extracted.
					reportProgress(opts, ProgressEvent{Layer: LayerSceneCards, Stage: "item-skip", ChapterID: ch.ID, SceneID: sc.ID, Current: sceneIndex + 1, Total: len(scenes), Message: fmt.Sprintf("Scene card %s %d/%d: already exists", sc.ID, sceneIndex+1, len(scenes))})
					continue
				} else if !errors.Is(err, store.ErrNotFound) {
					return total, recoveries, err
				}
			}

			sceneParagraphs := paragraphsInScene(paragraphs, paraByID, sc)
			if len(sceneParagraphs) == 0 {
				return total, recoveries, fmt.Errorf("collect scene-card context for %s: no paragraphs found from %q to %q", sc.ID, sc.ParagraphStart, sc.ParagraphEnd)
			}
			skipRecoveryOnFailure, promptTokens := sceneCardSkipsRecoveryOnInitialFailure(sc, paragraphs, sceneParagraphs, cfg)
			input := sceneCardWorkInput{
				ChapterID:             ch.ID,
				ChapterIndex:          chapterIndex,
				ChapterTotal:          len(chapters),
				SceneIndex:            sceneIndex,
				SceneTotal:            len(scenes),
				Scene:                 sc,
				Paragraphs:            sceneParagraphs,
				SkipRecoveryOnFailure: skipRecoveryOnFailure,
				PromptTokens:          promptTokens,
			}
			items = append(items, OrderedWorkItem[sceneCardWorkInput]{
				Sequence: sequence,
				TaskID:   sc.ID,
				Input:    input,
			})
			sequence++
		}

		err = RunOrderedWork(ctx, items, OrderedExecutorOptions{WorkerLimit: 1}, func(ctx context.Context, item OrderedWorkItem[sceneCardWorkInput]) (sceneCardWorkOutput, error) {
			input := item.Input
			events := make([]ProgressEvent, 0, 2)
			card, err := extractSceneCard(ctx, p, input.Scene, input.Paragraphs,
				opts.ExtractionProvider, opts.ExtractionModel, cfg, run, policy, input.SkipRecoveryOnFailure, func(event ProgressEvent) {
					events = append(events, event)
				})
			if err != nil {
				return sceneCardWorkOutput{}, fmt.Errorf("extract scene card for %s: %w", input.Scene.ID, err)
			}
			output := sceneCardWorkOutput{
				Input:    input,
				Card:     card,
				Progress: events,
			}
			if card.Recovery != nil && card.Status != SceneCardStatusSkipped {
				recovery := SceneCardRecoveryEvent{
					SceneID:   card.SceneID,
					ChapterID: input.Scene.ChapterID,
					Action:    card.Recovery.Action,
					Attempts:  card.Recovery.Attempts,
					Reason:    card.Recovery.Reason,
				}
				output.Recovery = &recovery
			}
			if staging != nil {
				ref, err := stageSceneCardWorkResult(staging, item.Sequence, output)
				if err != nil {
					return sceneCardWorkOutput{}, err
				}
				output.Staged = ref
			}
			return output, nil
		}, func(ctx context.Context, result OrderedWorkResult[sceneCardWorkOutput]) error {
			output := result.Output
			input := output.Input
			reportSceneCardWorkStart(opts, input)
			for _, event := range output.Progress {
				reportProgress(opts, event)
			}
			if output.Card.Status == SceneCardStatusSkipped {
				if err := committer.CommitSceneCard(output); err != nil {
					return err
				}
				reportProgress(opts, ProgressEvent{Layer: LayerSceneCards, Stage: "item-skip", ChapterID: input.ChapterID, SceneID: input.Scene.ID, Current: input.SceneIndex + 1, Total: input.SceneTotal, Message: fmt.Sprintf("Scene card %s %d/%d: skipped after initial failure for oversized full-chapter scene", input.Scene.ID, input.SceneIndex+1, input.SceneTotal)})
				return nil
			}

			if err := committer.CommitSceneCard(output); err != nil {
				return err
			}
			if output.Recovery != nil {
				recoveries = append(recoveries, *output.Recovery)
				reportProgress(opts, ProgressEvent{Layer: LayerSceneCards, Stage: "item-complete", ChapterID: input.ChapterID, SceneID: input.Scene.ID, Current: input.SceneIndex + 1, Total: input.SceneTotal, Message: fmt.Sprintf("Scene card %s %d/%d: completed with %s recovery", input.Scene.ID, input.SceneIndex+1, input.SceneTotal, output.Recovery.Action)})
			} else {
				reportProgress(opts, ProgressEvent{Layer: LayerSceneCards, Stage: "item-complete", ChapterID: input.ChapterID, SceneID: input.Scene.ID, Current: input.SceneIndex + 1, Total: input.SceneTotal, Message: fmt.Sprintf("Scene card %s %d/%d: completed", input.Scene.ID, input.SceneIndex+1, input.SceneTotal)})
			}
			total++
			return nil
		})
		if err != nil {
			return total, recoveries, err
		}
	}
	return total, recoveries, nil
}

func reportSceneCardWorkStart(opts Options, input sceneCardWorkInput) {
	if input.SkipRecoveryOnFailure {
		reportProgress(opts, ProgressEvent{Layer: LayerSceneCards, Stage: "item-start", ChapterID: input.ChapterID, SceneID: input.Scene.ID, Current: input.SceneIndex + 1, Total: input.SceneTotal, Message: fmt.Sprintf("Scene card %s %d/%d: full-chapter oversized prompt (~%d tokens); trying once without retry/fallback", input.Scene.ID, input.SceneIndex+1, input.SceneTotal, input.PromptTokens)})
		return
	}
	reportProgress(opts, ProgressEvent{Layer: LayerSceneCards, Stage: "item-start", ChapterID: input.ChapterID, SceneID: input.Scene.ID, Current: input.SceneIndex + 1, Total: input.SceneTotal, Message: fmt.Sprintf("Scene card %s %d/%d: extracting from %d paragraph(s)", input.Scene.ID, input.SceneIndex+1, input.SceneTotal, len(input.Paragraphs))})
}

func stageSceneCardWorkResult(staging *RunStagingStore, sequence int, output sceneCardWorkOutput) (StagedResultRef, error) {
	if staging == nil {
		return StagedResultRef{}, nil
	}
	return staging.StageJSON(LayerSceneCards, StagedResultMeta{
		Sequence:      sequence,
		TaskID:        output.Input.Scene.ID,
		TargetID:      output.Input.Scene.ID,
		SchemaVersion: 1,
	}, stagedSceneCardPayload{
		Card:     output.Card,
		Recovery: output.Recovery,
		Skipped:  output.Card != nil && output.Card.Status == SceneCardStatusSkipped,
	})
}
func sceneCardSkipsRecoveryOnInitialFailure(scene store.SceneRow, chapterParagraphs, sceneParagraphs []store.ParagraphRow, cfg sceneDetectConfig) (bool, int) {
	promptTokens := approxTokens(buildSceneCardPrompt(scene, sceneParagraphs))
	if !sceneSpansFullChapter(scene, chapterParagraphs) {
		return false, promptTokens
	}
	targetTokens := cfg.TargetContextTokens
	if targetTokens <= 0 {
		targetTokens = 12000
	}
	return promptTokens > targetTokens, promptTokens
}

func sceneSpansFullChapter(scene store.SceneRow, paragraphs []store.ParagraphRow) bool {
	if len(paragraphs) == 0 {
		return false
	}
	return scene.ParagraphStart == paragraphs[0].ID && scene.ParagraphEnd == paragraphs[len(paragraphs)-1].ID
}

func shouldSuggestSingleSceneChapterSplit(scenes []SceneRecord, paragraphs []store.ParagraphRow) bool {
	if len(scenes) != 1 || len(paragraphs) < minParagraphsForSingleSceneSplitSuggestion {
		return false
	}
	return scenes[0].ParagraphStart == paragraphs[0].ID && scenes[0].ParagraphEnd == paragraphs[len(paragraphs)-1].ID
}

func singleSceneChapterSplitSuggestion(chapterID string, paragraphs []store.ParagraphRow) string {
	breakAfter := suggestedMidpointBreakAfterParagraph(paragraphs)
	if breakAfter == "" {
		return fmt.Sprintf("Scenes %s: one scene spans the full chapter; consider adding an explicit scene break and rerun: story compile --layer scenes --chapter %s --force", chapterID, chapterID)
	}
	return fmt.Sprintf("Scenes %s: one scene spans the full chapter; consider adding an explicit scene break near the midpoint, for example after paragraph %s, then rerun: story compile --layer scenes --chapter %s --force", chapterID, breakAfter, chapterID)
}

func suggestedMidpointBreakAfterParagraph(paragraphs []store.ParagraphRow) string {
	if len(paragraphs) < 2 {
		return ""
	}
	return paragraphs[len(paragraphs)/2-1].ID
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
	if err := enc.Encode(v); err != nil {
		return err
	}
	return w.Sync()
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
