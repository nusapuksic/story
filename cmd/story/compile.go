package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/store"
)

func newCompileCmd() *cobra.Command {
	var (
		layer            string
		chapterID        string
		force            bool
		strictExtraction bool
	)
	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Compile the manuscript into a layered story model",
		Long: `Compile constructs the story model from the canonical manuscript.

Supported layers:
  scenes        Detect scene boundaries (explicit + optional LLM proposals)
  scene-cards   Extract structured scene cards using the configured LLM
  verification  Verify generated scene cards against cited manuscript evidence
  summaries     Generate chapter and book summaries using the configured LLM
  entities      Extract candidate entities and paragraph-level mentions

Without --layer, all implemented layers are run in order: scenes, scene-cards, verification when enabled, summaries, entities.

Scene-card extraction retries invalid model output once, retries timeouts with compact context, and then writes a deterministic fallback card. Use --strict-extraction for developer/debug runs that should fail immediately.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompile(layer, chapterID, force, strictExtraction)
		},
	}
	cmd.Flags().StringVar(&layer, "layer", "", "restrict to one layer: scenes, scene-cards, verification, summaries, or entities")
	cmd.Flags().StringVar(&chapterID, "chapter", "", "restrict to one chapter (e.g. ch-0001)")
	cmd.Flags().BoolVar(&force, "force", false, "recompute already-generated records")
	cmd.Flags().BoolVar(&strictExtraction, "strict-extraction", false, "fail on invalid scene-card model output or timeouts instead of retrying and falling back")
	cmd.AddCommand(newCompileStatusCmd())
	return cmd
}

// runCompile executes the compile pipeline.
func runCompile(layer, chapterID string, force, strictExtraction bool) error {
	p, err := openProject()
	if err != nil {
		return err
	}

	// Open or rebuild the SQLite index.
	st, err := openIndex(p)
	if err != nil {
		return err
	}
	defer st.Close()

	// Check that the manuscript has been imported.
	chapters, err := st.AllChapters()
	if err != nil {
		return err
	}
	if len(chapters) == 0 {
		return errors.New("no chapters found: run 'story import md' before compiling")
	}

	var extractProv provider.Provider
	var extractModel string
	if compileNeedsExtractionProvider(layer) {
		prov, model, provErr := provider.ForRole(p.Config.LLM, "extraction")
		if provErr == nil {
			extractProv = prov
			extractModel = model
		} else if !errors.Is(provErr, provider.ErrNoProvider) {
			return fmt.Errorf("load extraction provider: %w", provErr)
		}
		// ErrNoProvider is fine for optional scene proposals and will become a
		// clear compiler error for mandatory LLM-backed layers.
	}

	verificationMode, err := compiler.EffectiveVerificationMode(compiler.Options{}, p.Config.Compile)
	if err != nil {
		return err
	}

	var verifyProv provider.Provider
	var verifyModel string
	if compileNeedsVerificationProvider(layer, verificationMode) {
		prov, model, provErr := provider.ForRole(p.Config.LLM, "verification")
		if provErr == nil {
			verifyProv = prov
			verifyModel = model
		} else if !errors.Is(provErr, provider.ErrNoProvider) {
			return fmt.Errorf("load verification provider: %w", provErr)
		}
	}

	sceneCardFailurePolicy := p.Config.Compile.SceneCardFailurePolicy
	if strictExtraction {
		sceneCardFailurePolicy = compiler.SceneCardFailurePolicyStrict
	}

	opts := compiler.Options{
		Layer:                  layer,
		ChapterID:              chapterID,
		Force:                  force,
		SceneCardFailurePolicy: sceneCardFailurePolicy,
		Progress:               compileProgressPrinter(),
		ExtractionProvider:     extractProv,
		ExtractionModel:        extractModel,
		VerificationMode:       verificationMode,
		VerificationProvider:   verifyProv,
		VerificationModel:      verifyModel,
	}

	if !flags.jsonOut {
		info("Compiling manuscript (layer=%q, chapter=%q, force=%v, strict_extraction=%v, verification_mode=%q)...",
			layer, chapterID, force, strictExtraction, verificationMode)
	}

	result, err := compiler.Compile(nil, p, st, opts)
	if err != nil {
		return err
	}

	if flags.jsonOut {
		return printJSON(map[string]any{
			"run_id":                     result.RunID,
			"scenes_built":               result.ScenesBuilt,
			"cards_built":                result.CardsBuilt,
			"scene_card_recoveries":      result.SceneCardRecoveries,
			"scene_card_recovery_events": result.SceneCardRecoveryEvents,
			"entities_built":             result.EntitiesBuilt,
			"verifications_built":        result.VerificationsBuilt,
			"summaries_built":            result.SummariesBuilt,
		})
	}
	info("Run: %s", result.RunID)
	info("Scenes built:          %d", result.ScenesBuilt)
	info("Scene cards built:     %d", result.CardsBuilt)
	info("Scene card recoveries: %d", result.SceneCardRecoveries)
	printSceneCardRecoveryHints(result.SceneCardRecoveryEvents)
	info("Entities built:        %d", result.EntitiesBuilt)
	info("Verifications built: %d", result.VerificationsBuilt)
	info("Summaries built:     %d", result.SummariesBuilt)
	return nil
}

func compileProgressPrinter() compiler.ProgressFunc {
	if flags.jsonOut || flags.quiet {
		return nil
	}
	return func(event compiler.ProgressEvent) {
		if msg := strings.TrimSpace(event.Message); msg != "" {
			info("%s", msg)
		}
	}
}

type singleSceneChapterHint struct {
	ChapterID             string `json:"chapter_id"`
	SceneID               string `json:"scene_id"`
	Paragraphs            int    `json:"paragraphs"`
	BreakAfterParagraphID string `json:"break_after_paragraph_id,omitempty"`
}

const minParagraphsForSingleSceneStatusHint = 6

func indexedSingleSceneChapterHints(st *store.Store) ([]singleSceneChapterHint, error) {
	chapters, err := st.AllChapters()
	if err != nil {
		return nil, err
	}
	hints := []singleSceneChapterHint{}
	for _, ch := range chapters {
		scenes, err := st.ScenesByChapter(ch.ID)
		if err != nil {
			return nil, err
		}
		if len(scenes) != 1 {
			continue
		}
		paragraphs, err := st.ParagraphsByChapter(ch.ID)
		if err != nil {
			return nil, err
		}
		if len(paragraphs) < minParagraphsForSingleSceneStatusHint {
			continue
		}
		sc := scenes[0]
		if sc.ParagraphStart != paragraphs[0].ID || sc.ParagraphEnd != paragraphs[len(paragraphs)-1].ID {
			continue
		}
		hints = append(hints, singleSceneChapterHint{
			ChapterID:             ch.ID,
			SceneID:               sc.ID,
			Paragraphs:            len(paragraphs),
			BreakAfterParagraphID: paragraphs[len(paragraphs)/2-1].ID,
		})
	}
	return hints, nil
}

func printSingleSceneChapterHints(hints []singleSceneChapterHint) {
	if len(hints) == 0 {
		return
	}
	info("Chapters with one full-chapter scene:")
	for _, hint := range hints {
		info("  %s (%d paragraphs, scene %s): consider adding an explicit scene break near the midpoint, for example after paragraph %s", hint.ChapterID, hint.Paragraphs, hint.SceneID, hint.BreakAfterParagraphID)
	}
	info("Recreate scenes after editing with:")
	for _, hint := range hints {
		info("  story compile --layer scenes --chapter %s --force", hint.ChapterID)
	}
}

func indexedSceneCardRecoveryEvents(st *store.Store) ([]compiler.SceneCardRecoveryEvent, error) {
	scenes, err := st.AllScenes()
	if err != nil {
		return nil, err
	}
	chapterByScene := make(map[string]string, len(scenes))
	for _, sc := range scenes {
		chapterByScene[sc.ID] = sc.ChapterID
	}

	cards, err := st.AllSceneCards()
	if err != nil {
		return nil, err
	}
	events := []compiler.SceneCardRecoveryEvent{}
	for _, card := range cards {
		if strings.TrimSpace(card.RawJSON) == "" {
			continue
		}
		var raw struct {
			Recovery *compiler.SceneCardRecovery `json:"recovery"`
		}
		if err := json.Unmarshal([]byte(card.RawJSON), &raw); err != nil || raw.Recovery == nil {
			continue
		}
		events = append(events, compiler.SceneCardRecoveryEvent{
			SceneID:   card.SceneID,
			ChapterID: chapterByScene[card.SceneID],
			Action:    raw.Recovery.Action,
			Attempts:  raw.Recovery.Attempts,
			Reason:    raw.Recovery.Reason,
		})
	}
	return events, nil
}

func sceneCardRecoveryActionCount(events []compiler.SceneCardRecoveryEvent, action string) int {
	count := 0
	for _, event := range events {
		if event.Action == action {
			count++
		}
	}
	return count
}

func printSceneCardRecoveryHints(events []compiler.SceneCardRecoveryEvent) {
	if len(events) == 0 {
		return
	}
	info("Recovered scene cards:")
	for _, event := range events {
		info("  %s (%s): %s", event.SceneID, event.ChapterID, sceneCardRecoveryHint(event.Action))
	}
	info("Regenerate affected chapter(s) with:")
	for _, chapterID := range uniqueRecoveryChapters(events) {
		info("  story compile --layer scene-cards --chapter %s --force", chapterID)
	}
}

func sceneCardRecoveryHint(action string) string {
	switch action {
	case "fallback":
		return "fallback card, regeneration recommended"
	case "compact-retry":
		return "compact retry, review recommended"
	case "retry":
		return "validated after retry, review optional"
	default:
		return action + ", review recommended"
	}
}

func uniqueRecoveryChapters(events []compiler.SceneCardRecoveryEvent) []string {
	seen := make(map[string]bool, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.ChapterID) == "" {
			continue
		}
		seen[event.ChapterID] = true
	}
	chapters := make([]string, 0, len(seen))
	for chapterID := range seen {
		chapters = append(chapters, chapterID)
	}
	sort.Strings(chapters)
	return chapters
}

func compileNeedsExtractionProvider(layer string) bool {
	switch layer {
	case "", compiler.LayerScenes, compiler.LayerSceneCards, compiler.LayerEntities, compiler.LayerSummaries:
		return true
	default:
		return false
	}
}

func compileNeedsVerificationProvider(layer, verificationMode string) bool {
	return layer == compiler.LayerVerification || (layer == "" && verificationMode != compiler.VerificationModeOff)
}

// newCompileStatusCmd shows the current compilation status.
func newCompileStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show compilation status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := openProject()
			if err != nil {
				return err
			}
			st, err := openIndex(p)
			if err != nil {
				return err
			}
			defer st.Close()
			chapters, paragraphs, err := st.Counts()
			if err != nil {
				return err
			}
			scenes, cards, err := st.SceneCounts()
			if err != nil {
				return err
			}
			entities, mentions, err := st.EntityCounts()
			if err != nil {
				return err
			}
			recoveryEvents, err := indexedSceneCardRecoveryEvents(st)
			if err != nil {
				return err
			}
			fallbacks := sceneCardRecoveryActionCount(recoveryEvents, "fallback")
			sceneHints, err := indexedSingleSceneChapterHints(st)
			if err != nil {
				return err
			}
			if flags.jsonOut {
				return printJSON(map[string]any{
					"chapters":                         chapters,
					"paragraphs":                       paragraphs,
					"scenes":                           scenes,
					"scene_cards":                      cards,
					"single_scene_chapter_suggestions": sceneHints,
					"scene_card_recoveries":            len(recoveryEvents),
					"scene_card_fallbacks":             fallbacks,
					"scene_card_recovery_events":       recoveryEvents,
					"entities":                         entities,
					"mentions":                         mentions,
				})
			}
			info("Chapters:              %d", chapters)
			info("Paragraphs:            %d", paragraphs)
			info("Scenes:                %d", scenes)
			info("Scene split suggestions: %d", len(sceneHints))
			printSingleSceneChapterHints(sceneHints)
			info("Scene cards:           %d", cards)
			info("Scene card recoveries: %d", len(recoveryEvents))
			info("Scene card fallbacks:  %d", fallbacks)
			printSceneCardRecoveryHints(recoveryEvents)
			info("Entities:              %d", entities)
			info("Mentions:              %d", mentions)
			return nil
		},
	}
}
