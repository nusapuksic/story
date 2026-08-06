package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/query"
)

// exitInsufficientEvidence is returned when the query engine cannot gather
// enough evidence to answer the question (exit code 40, per cli-spec §15).
// This constant is declared in main.go; no re-declaration needed here.

func newAskCmd() *cobra.Command {
	var (
		mode             string
		chapterID        string
		maxEvidence      int
		includeGenerated bool
	)
	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask a question backed by manuscript evidence",
		Long: `Ask calls the configured discussion model with a bounded evidence packet
assembled from the indexed manuscript.

The engine retrieves relevant scene cards and paragraphs, constructs a
prompt, calls the model, validates cited paragraph identifiers, and returns
an answer with provenance.

Modes:
  recall          Factual events and explicit content  (default)
  continuity      Character knowledge and story state
  interpretation  Themes, motifs, and symbolic meaning
  style           Narrative voice and prose technique
  development     Character arcs and plot progression`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAsk(cmd.Context(), args[0], mode, chapterID, maxEvidence, includeGenerated)
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "recall",
		"query mode: recall, continuity, interpretation, style, development")
	cmd.Flags().StringVar(&chapterID, "chapter", "",
		"restrict evidence to a specific chapter (e.g. ch-0001)")
	cmd.Flags().IntVar(&maxEvidence, "max-evidence", 20,
		"maximum number of paragraphs in the evidence packet")
	cmd.Flags().BoolVar(&includeGenerated, "include-generated", false,
		"include scene cards with status 'generated' (not yet verified)")
	return cmd
}

func runAsk(ctx context.Context, question, mode, chapterID string, maxEvidence int, includeGenerated bool) error {
	p, err := openProject()
	if err != nil {
		return err
	}
	st, err := openIndex(p)
	if err != nil {
		return err
	}
	defer st.Close()

	recorder, err := newAskRunRecorder(p, askRunConfig{
		Question:         question,
		Mode:             mode,
		ChapterID:        chapterID,
		MaxEvidence:      maxEvidence,
		IncludeGenerated: includeGenerated,
	})
	if err != nil {
		return err
	}
	fail := func(runErr error) error {
		finishErr := recorder.finish(runErr, "")
		if finishErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("finalize ask run: %w", finishErr))
		}
		annotated := recorder.annotateError(runErr)
		if errors.Is(runErr, query.ErrInsufficientEvidence) {
			return &insufficientEvidenceError{annotated}
		}
		return annotated
	}

	// Load the discussion provider.
	prov, model, provErr := provider.ForRole(p.Config.LLM, "discussion")
	if provErr != nil {
		if errors.Is(provErr, provider.ErrNoProvider) {
			return fail(fmt.Errorf("no discussion provider configured: add [llm.roles.discussion] to story.toml"))
		}
		return fail(fmt.Errorf("load discussion provider: %w", provErr))
	}
	if err := recorder.setModel(model); err != nil {
		return fail(fmt.Errorf("record ask model: %w", err))
	}

	summaries, err := summaryContextForAsk(p, st, chapterID)
	if err != nil {
		return fail(err)
	}
	characterRoles, err := characterRoleContextForAsk(p)
	if err != nil {
		return fail(err)
	}

	opts := query.Options{
		QueryRunID:       recorder.id(),
		Mode:             mode,
		ChapterID:        chapterID,
		MaxEvidence:      maxEvidence,
		IncludeGenerated: includeGenerated,
		PromptsDir:       p.Path(project.PromptsDir),
		Summaries:        summaries,
		CharacterRoles:   characterRoles,
	}

	ans, err := query.Ask(ctx, st, &askRecordingProvider{inner: prov, recorder: recorder}, model, question, opts)
	if err != nil {
		return fail(err)
	}
	if err := recorder.finish(nil, ans.PromptVersion); err != nil {
		return fmt.Errorf("finalize ask run %s: %w", recorder.id(), err)
	}

	if flags.jsonOut {
		return printJSON(struct {
			*query.Answer
			RunDir string `json:"run_dir,omitempty"`
		}{Answer: ans, RunDir: recorder.runDir()})
	}

	// Human-readable output.
	info("%s", ans.Answer)
	if len(ans.Evidence) > 0 {
		info("\nEvidence:")
		for _, ev := range ans.Evidence {
			info("  [%s:%s]", ev.ChapterID, ev.ParagraphID)
		}
	}
	if len(ans.RecordsUsed) > 0 {
		info("\nRecords used:")
		for _, id := range ans.RecordsUsed {
			info("  [%s]", id)
		}
	}
	if len(ans.Uncertainties) > 0 {
		info("\nUncertainty:")
		for _, u := range ans.Uncertainties {
			info("  %s", u)
		}
	}
	info("\nRun: %s", ans.QueryRunID)
	info("Artifacts: %s", recorder.runDir())
	return nil
}

// insufficientEvidenceError wraps ErrInsufficientEvidence so the CLI can map
// it to exit code 40.
type insufficientEvidenceError struct{ cause error }

func (e *insufficientEvidenceError) Error() string { return e.cause.Error() }
func (e *insufficientEvidenceError) Unwrap() error { return e.cause }
