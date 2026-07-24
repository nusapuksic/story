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
  scenes       Detect scene boundaries (explicit + optional LLM proposals)
  scene-cards  Extract structured scene cards using the configured LLM
  summaries    Generate chapter and book summaries using the configured LLM

Without --layer, all implemented layers are run in order.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompile(layer, chapterID, force)
		},
	}
	cmd.Flags().StringVar(&layer, "layer", "", "restrict to one layer: scenes, scene-cards, or summaries")
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

	// Build extraction provider from config, if configured.
	var extractProv provider.Provider
	var extractModel string
	if layer == "" || layer == compiler.LayerSceneCards || layer == compiler.LayerSummaries {
		prov, model, provErr := provider.ForRole(p.Config.LLM, "extraction")
		if provErr == nil {
			extractProv = prov
			extractModel = model
		} else if !errors.Is(provErr, provider.ErrNoProvider) {
			return fmt.Errorf("load extraction provider: %w", provErr)
		}
		// ErrNoProvider is fine for LLM-backed layers (will fail gracefully
		// inside the compiler with a clear message).
	}

	opts := compiler.Options{
		Layer:              layer,
		ChapterID:          chapterID,
		Force:              force,
		ExtractionProvider: extractProv,
		ExtractionModel:    extractModel,
	}

	info("Compiling manuscript (layer=%q, chapter=%q, force=%v)…",
		layer, chapterID, force)

	result, err := compiler.Compile(nil, p, st, opts)
	if err != nil {
		return err
	}

	if flags.jsonOut {
		return printJSON(map[string]any{
			"run_id":          result.RunID,
			"scenes_built":    result.ScenesBuilt,
			"cards_built":     result.CardsBuilt,
			"summaries_built": result.SummariesBuilt,
		})
	}
	info("Run: %s", result.RunID)
	info("Scenes built:     %d", result.ScenesBuilt)
	info("Scene cards built: %d", result.CardsBuilt)
	info("Summaries built:   %d", result.SummariesBuilt)
	return nil
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
			if flags.jsonOut {
				return printJSON(map[string]any{
					"chapters":    chapters,
					"paragraphs":  paragraphs,
					"scenes":      scenes,
					"scene_cards": cards,
				})
			}
			info("Chapters:    %d", chapters)
			info("Paragraphs:  %d", paragraphs)
			info("Scenes:      %d", scenes)
			info("Scene cards: %d", cards)
			return nil
		},
	}
}
