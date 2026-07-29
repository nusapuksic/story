package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/provider"
)

func newCompileCmd() *cobra.Command {
	var (
		layer     string
		chapterID string
		force     bool
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

Without --layer, all implemented layers are run in order: scenes, scene-cards, verification when enabled, summaries, entities.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompile(layer, chapterID, force)
		},
	}
	cmd.Flags().StringVar(&layer, "layer", "", "restrict to one layer: scenes, scene-cards, verification, summaries, or entities")
	cmd.Flags().StringVar(&chapterID, "chapter", "", "restrict to one chapter (e.g. ch-0001)")
	cmd.Flags().BoolVar(&force, "force", false, "recompute already-generated records")
	cmd.AddCommand(newCompileStatusCmd())
	return cmd
}

// runCompile executes the compile pipeline.
func runCompile(layer, chapterID string, force bool) error {
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

	var verifyProv provider.Provider
	var verifyModel string
	if compileNeedsVerificationProvider(layer, p.Config.Compile.Verification) {
		prov, model, provErr := provider.ForRole(p.Config.LLM, "verification")
		if provErr == nil {
			verifyProv = prov
			verifyModel = model
		} else if !errors.Is(provErr, provider.ErrNoProvider) {
			return fmt.Errorf("load verification provider: %w", provErr)
		}
	}

	opts := compiler.Options{
		Layer:                layer,
		ChapterID:            chapterID,
		Force:                force,
		ExtractionProvider:   extractProv,
		ExtractionModel:      extractModel,
		VerificationProvider: verifyProv,
		VerificationModel:    verifyModel,
	}

	info("Compiling manuscript (layer=%q, chapter=%q, force=%v)...",
		layer, chapterID, force)

	result, err := compiler.Compile(nil, p, st, opts)
	if err != nil {
		return err
	}

	if flags.jsonOut {
		return printJSON(map[string]any{
			"run_id":              result.RunID,
			"scenes_built":        result.ScenesBuilt,
			"cards_built":         result.CardsBuilt,
			"entities_built":      result.EntitiesBuilt,
			"verifications_built": result.VerificationsBuilt,
			"summaries_built":     result.SummariesBuilt,
		})
	}
	info("Run: %s", result.RunID)
	info("Scenes built:        %d", result.ScenesBuilt)
	info("Scene cards built:   %d", result.CardsBuilt)
	info("Entities built:      %d", result.EntitiesBuilt)
	info("Verifications built: %d", result.VerificationsBuilt)
	info("Summaries built:     %d", result.SummariesBuilt)
	return nil
}

func compileNeedsExtractionProvider(layer string) bool {
	switch layer {
	case "", compiler.LayerScenes, compiler.LayerSceneCards, compiler.LayerEntities, compiler.LayerSummaries:
		return true
	default:
		return false
	}
}

func compileNeedsVerificationProvider(layer string, verificationEnabled bool) bool {
	return layer == compiler.LayerVerification || (layer == "" && verificationEnabled)
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
			if flags.jsonOut {
				return printJSON(map[string]any{
					"chapters":    chapters,
					"paragraphs":  paragraphs,
					"scenes":      scenes,
					"scene_cards": cards,
					"entities":    entities,
					"mentions":    mentions,
				})
			}
			info("Chapters:    %d", chapters)
			info("Paragraphs:  %d", paragraphs)
			info("Scenes:      %d", scenes)
			info("Scene cards: %d", cards)
			info("Entities:    %d", entities)
			info("Mentions:    %d", mentions)
			return nil
		},
	}
}
