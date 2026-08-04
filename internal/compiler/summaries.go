package compiler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/project"
	storyprompts "github.com/nusapuksic/story/internal/prompts"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/store"
)

// SummaryRecord represents one synthesis record in model/summaries.jsonl.
type SummaryRecord struct {
	RecordType    string            `json:"record_type"` // "chapter_summary" or "book_summary"
	ChapterID     string            `json:"chapter_id,omitempty"`
	ChapterTitle  string            `json:"chapter_title,omitempty"`
	Summary       string            `json:"summary"`
	Themes        []string          `json:"themes,omitempty"`
	Unresolved    []string          `json:"unresolved,omitempty"`
	Evidence      []string          `json:"evidence"`
	SourceRecords []string          `json:"source_records,omitempty"`
	Generation    SummaryGeneration `json:"generation"`
	Status        string            `json:"status"`
}

// SummaryGeneration is the provenance section of a summary record.
type SummaryGeneration struct {
	RunID         string `json:"run_id"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
	GeneratedAt   string `json:"generated_at"`
}

type rawSummary struct {
	Summary    flexibleString       `json:"summary"`
	Themes     flexibleStringList   `json:"themes"`
	Unresolved flexibleStringList   `json:"unresolved"`
	Evidence   flexibleEvidenceList `json:"evidence"`
}

type summaryIndex struct {
	Chapters map[string]SummaryRecord
	Book     *SummaryRecord
}

type principalCharacter struct {
	Name            string
	ChapterMentions int
}

const paragraphIDPatternSource = `p-[0-9A-HJKMNP-TV-Z]{26}`

var (
	paragraphIDPattern          = regexp.MustCompile(`\b` + paragraphIDPatternSource + `\b`)
	paragraphCitationBlockRegex = regexp.MustCompile(`\s*\[(?:\s*` + paragraphIDPatternSource + `\s*,?)+\s*\]`)
)

// compileSummaries writes chapter summaries and, for whole-project runs, a
// book summary to model/summaries.jsonl.
func compileSummaries(
	ctx context.Context,
	p *project.Project,
	st *store.Store,
	chapters []store.ChapterRow,
	opts Options,
	cfg sceneDetectConfig,
	run *Run,
) (int, error) {
	summariesPath := p.Path(filepath.Join(project.ModelDir, "summaries.jsonl"))
	idx, err := readSummaryIndex(summariesPath)
	if err != nil {
		return 0, err
	}
	staging, err := optionalRunStagingStore(run)
	if err != nil {
		return 0, err
	}

	summariesFile, err := openAppendJSONL(summariesPath)
	if err != nil {
		return 0, err
	}
	defer summariesFile.Close()
	committer := compileArtifactCommitter{staging: staging, summariesFile: summariesFile}

	items := make([]OrderedWorkItem[summaryWorkInput], 0, len(chapters))
	for chapterIndex, ch := range chapters {
		if !opts.Force {
			if existing, ok := idx.Chapters[ch.ID]; ok && strings.TrimSpace(existing.Summary) != "" {
				reportProgress(opts, ProgressEvent{Layer: LayerSummaries, Stage: "item-skip", ChapterID: ch.ID, Current: chapterIndex + 1, Total: len(chapters), Message: fmt.Sprintf("Summary %s (%d/%d): already exists", ch.ID, chapterIndex+1, len(chapters))})
				continue
			}
		}

		paragraphs, err := st.ParagraphsByChapter(ch.ID)
		if err != nil {
			return 0, err
		}
		if len(paragraphs) == 0 {
			reportProgress(opts, ProgressEvent{Layer: LayerSummaries, Stage: "item-skip", ChapterID: ch.ID, Current: chapterIndex + 1, Total: len(chapters), Message: fmt.Sprintf("Summary %s (%d/%d): no paragraphs", ch.ID, chapterIndex+1, len(chapters))})
			continue
		}

		items = append(items, OrderedWorkItem[summaryWorkInput]{
			Sequence: len(items),
			TaskID:   ch.ID,
			Input: summaryWorkInput{
				Chapter:      ch,
				ChapterIndex: chapterIndex,
				ChapterTotal: len(chapters),
				Paragraphs:   paragraphs,
			},
		})
	}

	total := 0
	chapterSummariesBuilt := 0
	err = RunOrderedWork(ctx, items, OrderedExecutorOptions{WorkerLimit: 1}, func(ctx context.Context, item OrderedWorkItem[summaryWorkInput]) (summaryWorkOutput, error) {
		input := item.Input
		rec, err := extractChapterSummary(ctx, p, input.Chapter, input.Paragraphs,
			opts.ExtractionProvider, opts.ExtractionModel, cfg, run)
		if err != nil {
			return summaryWorkOutput{}, fmt.Errorf("extract chapter summary for %s: %w", input.Chapter.ID, err)
		}
		output := summaryWorkOutput{Input: input, Record: rec}
		if staging != nil {
			ref, err := stageSummaryRecord(staging, item.Sequence, input.Chapter.ID, input.Chapter.ID, rec)
			if err != nil {
				return summaryWorkOutput{}, err
			}
			output.Staged = ref
		}
		return output, nil
	}, func(ctx context.Context, result OrderedWorkResult[summaryWorkOutput]) error {
		output := result.Output
		input := output.Input
		reportProgress(opts, ProgressEvent{Layer: LayerSummaries, Stage: "item-start", ChapterID: input.Chapter.ID, Current: input.ChapterIndex + 1, Total: input.ChapterTotal, Message: fmt.Sprintf("Summary %s (%d/%d): extracting from %d paragraph(s)", input.Chapter.ID, input.ChapterIndex+1, input.ChapterTotal, len(input.Paragraphs))})
		if err := committer.CommitSummary(output); err != nil {
			return err
		}
		idx.Chapters[input.Chapter.ID] = *output.Record
		chapterSummariesBuilt++
		total++
		reportProgress(opts, ProgressEvent{Layer: LayerSummaries, Stage: "item-complete", ChapterID: input.Chapter.ID, Current: input.ChapterIndex + 1, Total: input.ChapterTotal, Message: fmt.Sprintf("Summary %s (%d/%d): completed", input.Chapter.ID, input.ChapterIndex+1, input.ChapterTotal)})
		return nil
	})
	if err != nil {
		return total, err
	}

	if opts.ChapterID != "" {
		return total, nil
	}
	if !opts.Force && chapterSummariesBuilt == 0 && idx.Book != nil && strings.TrimSpace(idx.Book.Summary) != "" {
		return total, nil
	}

	chapterSummaries := orderedChapterSummaries(chapters, idx.Chapters)
	if len(chapterSummaries) == 0 {
		return total, nil
	}

	reportProgress(opts, ProgressEvent{Layer: LayerSummaries, Stage: "item-start", Message: fmt.Sprintf("Book summary: extracting from %d chapter summary record(s)", len(chapterSummaries))})
	principals, err := principalCharactersForBookSummary(st, chapters)
	if err != nil {
		return total, fmt.Errorf("resolve principal characters for book summary: %w", err)
	}

	book, err := extractBookSummary(ctx, p, chapterSummaries, principals,
		opts.ExtractionProvider, opts.ExtractionModel, cfg, run)
	if err != nil {
		return total, fmt.Errorf("extract book summary: %w", err)
	}
	var bookRef StagedResultRef
	if staging != nil {
		bookRef, err = stageSummaryRecord(staging, len(items), "book-summary", "book", book)
		if err != nil {
			return total, err
		}
	}
	if err := committer.CommitBookSummary(bookRef, book); err != nil {
		return total, err
	}
	total++
	reportProgress(opts, ProgressEvent{Layer: LayerSummaries, Stage: "item-complete", Message: "Book summary: completed"})
	return total, nil
}

func stageSummaryRecord(staging *RunStagingStore, sequence int, taskID, targetID string, record *SummaryRecord) (StagedResultRef, error) {
	if staging == nil {
		return StagedResultRef{}, nil
	}
	return staging.StageJSON(LayerSummaries, StagedResultMeta{
		Sequence:      sequence,
		TaskID:        taskID,
		TargetID:      targetID,
		SchemaVersion: 1,
	}, stagedSummaryPayload{Record: record})
}

const maxChapterSynthesisEvidencePerWindow = 3

func extractChapterSummary(
	ctx context.Context,
	p *project.Project,
	ch store.ChapterRow,
	paragraphs []store.ParagraphRow,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
) (*SummaryRecord, error) {
	windows := buildWindows(paragraphs, cfg.TargetContextTokens, cfg.OverlapParagraphs)
	if len(windows) <= 1 {
		return extractChapterSummaryWindow(ctx, p, ch, paragraphs, prov, model, cfg, run, 0, 0)
	}

	windowSummaries := make([]SummaryRecord, 0, len(windows))
	for i, win := range windows {
		rec, err := extractChapterSummaryWindow(ctx, p, ch, win.Paragraphs, prov, model, cfg, run, i+1, len(windows))
		if err != nil {
			return nil, err
		}
		windowSummaries = append(windowSummaries, *rec)
	}

	support := supportParagraphsForWindowSummaries(windows, windowSummaries)
	return synthesizeChapterSummary(ctx, p, ch, windowSummaries, support, prov, model, cfg, run)
}

func extractChapterSummaryWindow(
	ctx context.Context,
	p *project.Project,
	ch store.ChapterRow,
	paragraphs []store.ParagraphRow,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
	windowOrdinal int,
	windowCount int,
) (*SummaryRecord, error) {
	loadedPrompt := loadCompilerPrompt(p, storyprompts.ChapterSummary)
	systemPrompt, promptVersion := loadedPrompt.Content, loadedPrompt.Version
	pidSet := paragraphIDSet(paragraphs)
	fallbackSummary, fallbackEvidence := deriveChapterFallbackSummary(paragraphs)

	taskType := "chapter-summary"
	prompt := buildChapterSummaryPrompt(ch, paragraphs)
	errorPrefix := fmt.Sprintf("chapter summary LLM call for %s", ch.ID)
	if windowCount > 1 {
		taskType = "chapter-summary-window"
		prompt = buildChapterSummaryWindowPrompt(ch, paragraphs, windowOrdinal, windowCount)
		errorPrefix = fmt.Sprintf("chapter summary window %d/%d LLM call for %s", windowOrdinal, windowCount, ch.ID)
	}

	taskID := ids.NewTaskID()
	req := provider.GenerationRequest{
		Model: model,
		Messages: []provider.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxOutputTokens,
		JSONMode:    true,
	}

	resp, timing, err := generateWithAudit(ctx, run, taskID, prov, req)
	if err != nil {
		recordSummaryTask(run, taskID, taskType, ch.ID, TaskStatusFailed, err.Error(), timing)
		return nil, fmt.Errorf("%s: %w", errorPrefix, err)
	}

	rec, parseErr := parseSummaryResponse(resp.Content, "chapter_summary", ch.ID, ch.Title, nil,
		pidSet, fallbackSummary, fallbackEvidence, runID(run), model, promptVersion)
	status := TaskStatusCompleted
	errMsg := ""
	if parseErr != nil {
		status = TaskStatusFailed
		errMsg = parseErr.Error()
	}
	recordSummaryTask(run, taskID, taskType, ch.ID, status, errMsg, timing)
	return rec, parseErr
}

func synthesizeChapterSummary(
	ctx context.Context,
	p *project.Project,
	ch store.ChapterRow,
	windowSummaries []SummaryRecord,
	support []store.ParagraphRow,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
) (*SummaryRecord, error) {
	loadedPrompt := loadCompilerPrompt(p, storyprompts.ChapterSummary)
	systemPrompt, promptVersion := loadedPrompt.Content, loadedPrompt.Version
	pidSet := paragraphIDSet(support)
	fallbackSummary, fallbackEvidence := deriveBookFallbackSummary(windowSummaries, support)

	taskID := ids.NewTaskID()
	req := provider.GenerationRequest{
		Model: model,
		Messages: []provider.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: buildChapterSummarySynthesisPrompt(ch, windowSummaries, support)},
		},
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxOutputTokens,
		JSONMode:    true,
	}

	resp, timing, err := generateWithAudit(ctx, run, taskID, prov, req)
	if err != nil {
		recordSummaryTask(run, taskID, "chapter-summary", ch.ID, TaskStatusFailed, err.Error(), timing)
		return nil, fmt.Errorf("chapter summary synthesis LLM call for %s: %w", ch.ID, err)
	}

	rec, parseErr := parseSummaryResponse(resp.Content, "chapter_summary", ch.ID, ch.Title, nil,
		pidSet, fallbackSummary, fallbackEvidence, runID(run), model, promptVersion)
	status := TaskStatusCompleted
	errMsg := ""
	if parseErr != nil {
		status = TaskStatusFailed
		errMsg = parseErr.Error()
	}
	recordSummaryTask(run, taskID, "chapter-summary", ch.ID, status, errMsg, timing)
	return rec, parseErr
}

func supportParagraphsForWindowSummaries(windows []Window, summaries []SummaryRecord) []store.ParagraphRow {
	paragraphByID := make(map[string]store.ParagraphRow)
	for _, win := range windows {
		for _, pp := range win.Paragraphs {
			paragraphByID[pp.ID] = pp
		}
	}

	seen := make(map[string]bool)
	out := make([]store.ParagraphRow, 0, len(windows))
	for i, win := range windows {
		appendedForWindow := false
		if i < len(summaries) {
			addedFromSummary := 0
			for _, pid := range summaries[i].Evidence {
				pp, ok := paragraphByID[pid]
				if !ok {
					continue
				}
				if !seen[pid] && addedFromSummary < maxChapterSynthesisEvidencePerWindow {
					seen[pid] = true
					out = append(out, pp)
					appendedForWindow = true
				}
				addedFromSummary++
			}
		}
		if appendedForWindow {
			continue
		}
		for _, pp := range win.Paragraphs {
			if !seen[pp.ID] {
				seen[pp.ID] = true
				out = append(out, pp)
				break
			}
		}
	}
	return out
}
func extractBookSummary(
	ctx context.Context,
	p *project.Project,
	chapterSummaries []SummaryRecord,
	principals []principalCharacter,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
) (*SummaryRecord, error) {
	loadedPrompt := loadCompilerPrompt(p, storyprompts.BookSummary)
	systemPrompt, promptVersion := loadedPrompt.Content, loadedPrompt.Version
	sourceRecords := make([]string, 0, len(chapterSummaries))
	validEvidenceIDs := make(map[string]bool, len(chapterSummaries))
	for _, rec := range chapterSummaries {
		if rec.ChapterID != "" {
			sourceRecords = append(sourceRecords, rec.ChapterID)
			validEvidenceIDs[rec.ChapterID] = true
		}
	}
	fallbackSummary, fallbackEvidence := deriveBookFallbackFromChapterSummaries(chapterSummaries)

	taskID := ids.NewTaskID()
	req := provider.GenerationRequest{
		Model: model,
		Messages: []provider.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: buildBookSummaryPrompt(p.Config.Title, chapterSummaries, principals)},
		},
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxOutputTokens,
		JSONMode:    true,
	}

	resp, timing, err := generateWithAudit(ctx, run, taskID, prov, req)
	if err != nil {
		recordSummaryTask(run, taskID, "book-summary", "", TaskStatusFailed, err.Error(), timing)
		return nil, fmt.Errorf("book summary LLM call: %w", err)
	}

	rec, parseErr := parseSummaryResponse(resp.Content, "book_summary", "", "", sourceRecords,
		validEvidenceIDs, fallbackSummary, fallbackEvidence, runID(run), model, promptVersion)
	if parseErr == nil {
		parseErr = validateBookSummaryCoverage(rec, sourceRecords, principals)
	}
	status := TaskStatusCompleted
	errMsg := ""
	if parseErr != nil {
		status = TaskStatusFailed
		errMsg = parseErr.Error()
	}
	recordSummaryTask(run, taskID, "book-summary", "", status, errMsg, timing)
	return rec, parseErr
}
func parseSummaryResponse(
	content, recordType, chapterID, chapterTitle string,
	sourceRecords []string,
	validPIDs map[string]bool,
	fallbackSummary string,
	fallbackEvidence []string,
	runID, model, promptVersion string,
) (*SummaryRecord, error) {
	content = stripJSONFences(content)
	var raw rawSummary
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		if isTruncatedJSONError(err) {
			fallbackSummary = strings.TrimSpace(fallbackSummary)
			if fallbackSummary == "" {
				return nil, fmt.Errorf("%s response missing summary", recordType)
			}
			return fallbackSummaryRecord(
				recordType, chapterID, chapterTitle, sourceRecords,
				fallbackSummary, fallbackEvidenceForSet(fallbackEvidence, validPIDs),
				runID, model, promptVersion,
			), nil
		}
		return nil, fmt.Errorf("parse %s response: %w", recordType, err)
	}
	if strings.TrimSpace(string(raw.Summary)) == "" {
		if nested, ok := nestedRawSummary(content, recordType); ok {
			raw = nested
		}
	}

	summary := strings.TrimSpace(string(raw.Summary))
	evidenceCandidates := []string(raw.Evidence)
	if recordType == "chapter_summary" {
		evidenceCandidates = summaryEvidenceCandidates(evidenceCandidates, summary)
	}
	evidence, err := validateSummaryEvidence(evidenceCandidates, validPIDs, recordType)
	if err != nil {
		return nil, err
	}
	if summary == "" {
		summary = fallbackSummary
		if len(evidence) == 0 {
			evidence = fallbackEvidenceForSet(fallbackEvidence, validPIDs)
		}
	}
	if summary == "" {
		return nil, fmt.Errorf("%s response missing summary", recordType)
	}

	return &SummaryRecord{
		RecordType:    recordType,
		ChapterID:     chapterID,
		ChapterTitle:  chapterTitle,
		Summary:       summary,
		Themes:        []string(raw.Themes),
		Unresolved:    []string(raw.Unresolved),
		Evidence:      evidence,
		SourceRecords: sourceRecords,
		Generation: SummaryGeneration{
			RunID:         runID,
			Model:         model,
			PromptVersion: promptVersion,
			GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		},
		Status: "generated",
	}, nil
}

func fallbackSummaryRecord(
	recordType, chapterID, chapterTitle string,
	sourceRecords []string,
	summary string,
	evidence []string,
	runID, model, promptVersion string,
) *SummaryRecord {
	return &SummaryRecord{
		RecordType:    recordType,
		ChapterID:     chapterID,
		ChapterTitle:  chapterTitle,
		Summary:       summary,
		Evidence:      evidence,
		SourceRecords: sourceRecords,
		Generation: SummaryGeneration{
			RunID:         runID,
			Model:         model,
			PromptVersion: promptVersion,
			GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		},
		Status: "generated",
	}
}

func readSummaryIndex(path string) (summaryIndex, error) {
	idx := summaryIndex{Chapters: make(map[string]SummaryRecord)}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return idx, nil
	}
	if err != nil {
		return idx, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec SummaryRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		switch rec.RecordType {
		case "chapter_summary":
			if rec.ChapterID != "" {
				idx.Chapters[rec.ChapterID] = rec
			}
		case "book_summary":
			rec := rec
			idx.Book = &rec
		}
	}
	return idx, sc.Err()
}

func orderedChapterSummaries(chapters []store.ChapterRow, byID map[string]SummaryRecord) []SummaryRecord {
	out := make([]SummaryRecord, 0, len(chapters))
	for _, ch := range chapters {
		if rec, ok := byID[ch.ID]; ok && strings.TrimSpace(rec.Summary) != "" {
			out = append(out, rec)
		}
	}
	return out
}

func paragraphIDSet(paragraphs []store.ParagraphRow) map[string]bool {
	out := make(map[string]bool, len(paragraphs))
	for _, pp := range paragraphs {
		out[pp.ID] = true
	}
	return out
}

func summaryEvidenceCandidates(evidence []string, summary string) []string {
	candidates := append([]string{}, evidence...)
	candidates = append(candidates, paragraphIDPattern.FindAllString(summary, -1)...)
	return dedupeStrings(candidates)
}

func chapterSummaryTextForBookPrompt(summary string) string {
	summary = paragraphCitationBlockRegex.ReplaceAllString(summary, "")
	summary = paragraphIDPattern.ReplaceAllString(summary, "")
	return strings.Join(strings.Fields(summary), " ")
}

func validateSummaryEvidence(evidence []string, validPIDs map[string]bool, recordType string) ([]string, error) {
	seen := make(map[string]bool, len(evidence))
	out := make([]string, 0, len(evidence))
	for _, pid := range evidence {
		pid = strings.TrimSpace(pid)
		if pid == "" {
			continue
		}
		if !validPIDs[pid] {
			return nil, fmt.Errorf("%s cites unknown evidence ID %q", recordType, pid)
		}
		if !seen[pid] {
			seen[pid] = true
			out = append(out, pid)
		}
	}
	return out, nil
}

func fallbackEvidenceForSet(evidence []string, validPIDs map[string]bool) []string {
	out := make([]string, 0, len(evidence))
	seen := make(map[string]bool, len(evidence))
	for _, pid := range evidence {
		if validPIDs[pid] && !seen[pid] {
			seen[pid] = true
			out = append(out, pid)
		}
	}
	return out
}

func nestedRawSummary(content, recordType string) (rawSummary, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &obj); err != nil {
		return rawSummary{}, false
	}
	keys := []string{recordType, "summary_record", "result"}
	for _, key := range keys {
		data, ok := obj[key]
		if !ok {
			continue
		}
		var raw rawSummary
		if err := json.Unmarshal(data, &raw); err == nil && strings.TrimSpace(string(raw.Summary)) != "" {
			return raw, true
		}
	}
	return rawSummary{}, false
}

func stripJSONFences(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		if i := strings.Index(content, "\n"); i >= 0 {
			content = content[i+1:]
		}
		if i := strings.LastIndex(content, "```"); i >= 0 {
			content = content[:i]
		}
		content = strings.TrimSpace(content)
	}
	return content
}

func buildChapterSummaryPrompt(ch store.ChapterRow, paragraphs []store.ParagraphRow) string {
	var sb strings.Builder
	sb.WriteString("Summarize this chapter as evidence-backed JSON.\n")
	sb.WriteString("Chapter ID: ")
	sb.WriteString(ch.ID)
	sb.WriteString("\nTitle: ")
	sb.WriteString(ch.Title)
	sb.WriteString("\nReturn JSON matching the schema:\n")
	sb.WriteString(`{"summary":"...","themes":[],"unresolved":[],"evidence":["p-..."]}`)
	sb.WriteString("\nCite paragraph IDs for concrete claims. Use only IDs from the list below.\n\n")
	writeParagraphExcerpts(&sb, paragraphs)
	return sb.String()
}

func buildChapterSummaryWindowPrompt(ch store.ChapterRow, paragraphs []store.ParagraphRow, windowOrdinal, windowCount int) string {
	var sb strings.Builder
	sb.WriteString("Summarize this chapter window as evidence-backed JSON.\n")
	sb.WriteString("This window is one contiguous part of a single chapter, not the whole book.\n")
	sb.WriteString("Chapter ID: ")
	sb.WriteString(ch.ID)
	sb.WriteString("\nTitle: ")
	sb.WriteString(ch.Title)
	sb.WriteString("\nWindow: ")
	sb.WriteString(fmt.Sprintf("%d of %d", windowOrdinal, windowCount))
	sb.WriteString("\nReturn JSON matching the schema:\n")
	sb.WriteString(`{"summary":"...","themes":[],"unresolved":[],"evidence":["p-..."]}`)
	sb.WriteString("\nCite paragraph IDs for concrete claims. Use only IDs from the list below.\n\n")
	writeParagraphExcerpts(&sb, paragraphs)
	return sb.String()
}

func buildChapterSummarySynthesisPrompt(
	ch store.ChapterRow,
	windowSummaries []SummaryRecord,
	support []store.ParagraphRow,
) string {
	var sb strings.Builder
	sb.WriteString("Merge chapter-window summaries into one evidence-backed chapter summary as JSON.\n")
	sb.WriteString("Chapter ID: ")
	sb.WriteString(ch.ID)
	sb.WriteString("\nTitle: ")
	sb.WriteString(ch.Title)
	sb.WriteString("\nReturn JSON matching the schema:\n")
	sb.WriteString(`{"summary":"...","themes":[],"unresolved":[],"evidence":["p-..."]}`)
	sb.WriteString("\nCite paragraph IDs for concrete claims. Use only IDs from the supporting excerpts below.\n\n")
	sb.WriteString("Window summaries:\n")
	for i, rec := range windowSummaries {
		sb.WriteString("- Window ")
		sb.WriteString(fmt.Sprintf("%d", i+1))
		sb.WriteString(": ")
		sb.WriteString(rec.Summary)
		if len(rec.Evidence) > 0 {
			sb.WriteString(" Evidence: ")
			sb.WriteString(strings.Join(rec.Evidence, ", "))
		}
		if len(rec.Themes) > 0 {
			sb.WriteString(" Themes: ")
			sb.WriteString(strings.Join(rec.Themes, ", "))
		}
		if len(rec.Unresolved) > 0 {
			sb.WriteString(" Unresolved: ")
			sb.WriteString(strings.Join(rec.Unresolved, ", "))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nSupporting paragraph excerpts:\n")
	writeParagraphExcerpts(&sb, support)
	return sb.String()
}

func writeParagraphExcerpts(sb *strings.Builder, paragraphs []store.ParagraphRow) {
	for _, pp := range paragraphs {
		sb.WriteString("--- ")
		sb.WriteString(pp.ID)
		sb.WriteString(" ---\n")
		sb.WriteString(pp.Text)
		sb.WriteString("\n\n")
	}
}
func buildBookSummaryPrompt(title string, summaries []SummaryRecord, principals []principalCharacter) string {
	var sb strings.Builder
	sb.WriteString("Produce a comprehensive editorial synopsis from chapter summary records as evidence-backed JSON.\n")
	if title != "" {
		sb.WriteString("Book title: ")
		sb.WriteString(title)
		sb.WriteString("\n")
	}
	sb.WriteString("Coverage requirements:\n")
	sb.WriteString("- Mention every chapter.\n")
	sb.WriteString("- Mention every major turning point.\n")
	sb.WriteString("- Mention every permanent character introduction.\n")
	sb.WriteString("- Mention every death.\n")
	sb.WriteString("- Mention every revelation.\n")
	sb.WriteString("- Mention every location change.\n")
	sb.WriteString("- Include the final state of every principal character listed below.\n")
	sb.WriteString("- Compress prose, not information.\n")
	sb.WriteString("Return JSON matching the schema:\n")
	sb.WriteString(`{"summary":"...","themes":[],"unresolved":[],"evidence":["ch-..."]}`)
	sb.WriteString("\nCite only chapter IDs from the records below. Do not cite paragraph IDs.\n")
	sb.WriteString("Use unresolved only for questions or tensions that remain open at book-summary level.\n\n")
	sb.WriteString("Required chapter coverage:\n")
	for _, rec := range summaries {
		if rec.ChapterID == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(rec.ChapterID)
		if rec.ChapterTitle != "" {
			sb.WriteString(" (")
			sb.WriteString(rec.ChapterTitle)
			sb.WriteString(")")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nPrincipal characters requiring final-state coverage:\n")
	if len(principals) == 0 {
		sb.WriteString("- (none detected from compiled entities)\n")
	} else {
		for _, principal := range principals {
			sb.WriteString("- ")
			sb.WriteString(principal.Name)
			sb.WriteString(" (mentioned in ")
			sb.WriteString(fmt.Sprintf("%d", principal.ChapterMentions))
			sb.WriteString(" chapter(s))\n")
		}
	}
	sb.WriteString("\n")
	sb.WriteString("Chapter summary records:\n")
	for _, rec := range summaries {
		sb.WriteString("- ")
		sb.WriteString(rec.ChapterID)
		if rec.ChapterTitle != "" {
			sb.WriteString(" (")
			sb.WriteString(rec.ChapterTitle)
			sb.WriteString(")")
		}
		sb.WriteString("\n  Summary: ")
		sb.WriteString(chapterSummaryTextForBookPrompt(rec.Summary))
		if len(rec.Themes) > 0 {
			sb.WriteString("\n  Themes: ")
			sb.WriteString(strings.Join(rec.Themes, ", "))
		}
		if len(rec.Unresolved) > 0 {
			sb.WriteString("\n  Unresolved: ")
			sb.WriteString(strings.Join(rec.Unresolved, ", "))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func validateBookSummaryCoverage(rec *SummaryRecord, chapterIDs []string, principals []principalCharacter) error {
	if rec == nil {
		return fmt.Errorf("book summary is missing")
	}
	evidence := make(map[string]bool, len(rec.Evidence))
	for _, id := range rec.Evidence {
		evidence[strings.TrimSpace(id)] = true
	}
	for _, chapterID := range chapterIDs {
		if strings.TrimSpace(chapterID) == "" {
			continue
		}
		if !evidence[chapterID] {
			return fmt.Errorf("book summary missing chapter evidence %q", chapterID)
		}
	}

	summaryLower := strings.ToLower(rec.Summary)
	for _, principal := range principals {
		name := strings.TrimSpace(principal.Name)
		if name == "" {
			continue
		}
		if !strings.Contains(summaryLower, strings.ToLower(name)) {
			return fmt.Errorf("book summary missing final state for principal character %q", name)
		}
	}
	return nil
}

func principalCharactersForBookSummary(st *store.Store, chapters []store.ChapterRow) ([]principalCharacter, error) {
	if st == nil || len(chapters) == 0 {
		return nil, nil
	}

	selectedChapters := make(map[string]bool, len(chapters))
	for _, ch := range chapters {
		if strings.TrimSpace(ch.ID) != "" {
			selectedChapters[ch.ID] = true
		}
	}

	rows, err := st.EntityRowsForChapter("")
	if err != nil {
		return nil, err
	}

	type tally struct {
		Name     string
		Chapters map[string]bool
	}
	byName := make(map[string]*tally)
	for _, row := range rows {
		if !selectedChapters[row.ChapterID] {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(row.Type), "character") {
			continue
		}
		name := strings.TrimSpace(row.CanonicalName)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		current, ok := byName[key]
		if !ok {
			current = &tally{Name: name, Chapters: make(map[string]bool)}
			byName[key] = current
		}
		current.Chapters[row.ChapterID] = true
	}
	for _, ch := range chapters {
		refs, err := st.ReverseIndexRefsForChapter(ch.ID, []string{store.ReverseTermPOV})
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			name := strings.TrimSpace(ref.RawValue)
			if name == "" {
				name = strings.TrimSpace(ref.Term)
			}
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			current, ok := byName[key]
			if !ok {
				current = &tally{Name: name, Chapters: make(map[string]bool)}
				byName[key] = current
			}
			current.Chapters[ch.ID] = true
		}
	}

	threshold := (len(selectedChapters) + 1) / 2
	out := make([]principalCharacter, 0, len(byName))
	for _, entry := range byName {
		count := len(entry.Chapters)
		if count >= threshold {
			out = append(out, principalCharacter{
				Name:            entry.Name,
				ChapterMentions: count,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChapterMentions != out[j].ChapterMentions {
			return out[i].ChapterMentions > out[j].ChapterMentions
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}
func deriveChapterFallbackSummary(paragraphs []store.ParagraphRow) (string, []string) {
	parts := make([]string, 0, 2)
	evidence := make([]string, 0, 2)
	for _, pp := range paragraphs {
		text := firstSentence(pp.Text, 260)
		if text == "" {
			continue
		}
		parts = append(parts, text)
		evidence = append(evidence, pp.ID)
		if len(parts) == 2 {
			break
		}
	}
	return strings.Join(parts, " "), evidence
}

func deriveBookFallbackFromChapterSummaries(summaries []SummaryRecord) (string, []string) {
	parts := make([]string, 0, 3)
	evidence := make([]string, 0, 3)
	for _, rec := range summaries {
		text := firstSentence(rec.Summary, 260)
		if text != "" && len(parts) < 3 {
			parts = append(parts, text)
		}
		if rec.ChapterID != "" && len(evidence) < 3 {
			evidence = append(evidence, rec.ChapterID)
		}
		if len(parts) == 3 && len(evidence) == 3 {
			break
		}
	}
	return strings.Join(parts, " "), evidence
}

func deriveBookFallbackSummary(summaries []SummaryRecord, support []store.ParagraphRow) (string, []string) {
	parts := make([]string, 0, 3)
	for _, rec := range summaries {
		text := firstSentence(rec.Summary, 260)
		if text == "" {
			continue
		}
		parts = append(parts, text)
		if len(parts) == 3 {
			break
		}
	}
	evidence := make([]string, 0, len(support))
	for _, pp := range support {
		if pp.ID != "" {
			evidence = append(evidence, pp.ID)
		}
		if len(evidence) == 3 {
			break
		}
	}
	return strings.Join(parts, " "), evidence
}

func firstSentence(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ""
	}
	if i := strings.IndexAny(text, ".!?"); i >= 0 {
		text = text[:i+1]
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	text = string(runes[:maxRunes])
	if i := strings.LastIndex(text, " "); i > 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text) + "..."
}

func recordSummaryTask(run *Run, taskID, taskType, chapterID, status, errMsg string, timings ...taskTiming) {
	if run == nil {
		return
	}
	record := TaskRecord{
		TaskID:    taskID,
		RunID:     runID(run),
		TaskType:  taskType,
		ChapterID: chapterID,
		Status:    status,
		Error:     errMsg,
	}
	if len(timings) > 0 {
		timings[0].applyTo(&record)
	}
	_ = run.recordTask(record)
}
