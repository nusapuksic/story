package compiler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/project"
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
	Summary    flexibleString     `json:"summary"`
	Themes     flexibleStringList `json:"themes"`
	Unresolved flexibleStringList `json:"unresolved"`
	Evidence   []string           `json:"evidence"`
}

type summaryIndex struct {
	Chapters map[string]SummaryRecord
	Book     *SummaryRecord
}

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

	summariesFile, err := openAppendJSONL(summariesPath)
	if err != nil {
		return 0, err
	}
	defer summariesFile.Close()

	total := 0
	chapterSummariesBuilt := 0
	for _, ch := range chapters {
		if !opts.Force {
			if existing, ok := idx.Chapters[ch.ID]; ok && strings.TrimSpace(existing.Summary) != "" {
				continue
			}
		}

		paragraphs, err := st.ParagraphsByChapter(ch.ID)
		if err != nil {
			return total, err
		}
		if len(paragraphs) == 0 {
			continue
		}

		rec, err := extractChapterSummary(ctx, p, ch, paragraphs,
			opts.ExtractionProvider, opts.ExtractionModel, cfg, run)
		if err != nil {
			return total, fmt.Errorf("extract chapter summary for %s: %w", ch.ID, err)
		}
		if err := appendJSONL(summariesFile, rec); err != nil {
			return total, err
		}
		idx.Chapters[ch.ID] = *rec
		chapterSummariesBuilt++
		total++
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

	book, err := extractBookSummary(ctx, p, st, chapters, chapterSummaries,
		opts.ExtractionProvider, opts.ExtractionModel, cfg, run)
	if err != nil {
		return total, fmt.Errorf("extract book summary: %w", err)
	}
	if err := appendJSONL(summariesFile, book); err != nil {
		return total, err
	}
	total++
	return total, nil
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
	systemPrompt, promptVersion := loadSummaryPrompt(p, "chapter-summary.md",
		"chapter-summary-v1", defaultChapterSummarySystemPrompt)
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

	resp, err := prov.Generate(ctx, req)
	if run != nil {
		_ = run.saveRawResponse(taskID, resp.Content)
	}
	if err != nil {
		recordSummaryTask(run, taskID, taskType, ch.ID, TaskStatusFailed, err.Error())
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
	recordSummaryTask(run, taskID, taskType, ch.ID, status, errMsg)
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
	systemPrompt, promptVersion := loadSummaryPrompt(p, "chapter-summary.md",
		"chapter-summary-v1", defaultChapterSummarySystemPrompt)
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

	resp, err := prov.Generate(ctx, req)
	if run != nil {
		_ = run.saveRawResponse(taskID, resp.Content)
	}
	if err != nil {
		recordSummaryTask(run, taskID, "chapter-summary", ch.ID, TaskStatusFailed, err.Error())
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
	recordSummaryTask(run, taskID, "chapter-summary", ch.ID, status, errMsg)
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
	st *store.Store,
	chapters []store.ChapterRow,
	chapterSummaries []SummaryRecord,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
) (*SummaryRecord, error) {
	systemPrompt, promptVersion := loadSummaryPrompt(p, "book-summary.md",
		"book-summary-v1", defaultBookSummarySystemPrompt)
	support, err := bookEvidenceParagraphs(st, chapters, chapterSummaries)
	if err != nil {
		return nil, err
	}
	pidSet := paragraphIDSet(support)
	sourceRecords := make([]string, 0, len(chapterSummaries))
	for _, rec := range chapterSummaries {
		if rec.ChapterID != "" {
			sourceRecords = append(sourceRecords, rec.ChapterID)
		}
	}
	fallbackSummary, fallbackEvidence := deriveBookFallbackSummary(chapterSummaries, support)

	taskID := ids.NewTaskID()
	req := provider.GenerationRequest{
		Model: model,
		Messages: []provider.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: buildBookSummaryPrompt(p.Config.Title, chapterSummaries, support)},
		},
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxOutputTokens,
		JSONMode:    true,
	}

	resp, err := prov.Generate(ctx, req)
	if run != nil {
		_ = run.saveRawResponse(taskID, resp.Content)
	}
	if err != nil {
		recordSummaryTask(run, taskID, "book-summary", "", TaskStatusFailed, err.Error())
		return nil, fmt.Errorf("book summary LLM call: %w", err)
	}

	rec, parseErr := parseSummaryResponse(resp.Content, "book_summary", "", "", sourceRecords,
		pidSet, fallbackSummary, fallbackEvidence, runID(run), model, promptVersion)
	status := TaskStatusCompleted
	errMsg := ""
	if parseErr != nil {
		status = TaskStatusFailed
		errMsg = parseErr.Error()
	}
	recordSummaryTask(run, taskID, "book-summary", "", status, errMsg)
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
		return nil, fmt.Errorf("parse %s response: %w", recordType, err)
	}
	if strings.TrimSpace(string(raw.Summary)) == "" {
		if nested, ok := nestedRawSummary(content, recordType); ok {
			raw = nested
		}
	}

	summary := strings.TrimSpace(string(raw.Summary))
	evidence, err := validateSummaryEvidence(raw.Evidence, validPIDs, recordType)
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

func bookEvidenceParagraphs(
	st *store.Store,
	chapters []store.ChapterRow,
	summaries []SummaryRecord,
) ([]store.ParagraphRow, error) {
	summaryByChapter := make(map[string]SummaryRecord, len(summaries))
	wanted := make(map[string]bool)
	for _, rec := range summaries {
		if rec.ChapterID != "" {
			summaryByChapter[rec.ChapterID] = rec
		}
		for _, pid := range rec.Evidence {
			wanted[pid] = true
		}
	}

	var out []store.ParagraphRow
	for _, ch := range chapters {
		paragraphs, err := st.ParagraphsByChapter(ch.ID)
		if err != nil {
			return nil, err
		}
		if len(paragraphs) == 0 {
			continue
		}
		rec := summaryByChapter[ch.ID]
		if len(rec.Evidence) == 0 {
			out = append(out, paragraphs[0])
			continue
		}
		for _, pp := range paragraphs {
			if wanted[pp.ID] {
				out = append(out, pp)
			}
		}
	}
	return out, nil
}

func paragraphIDSet(paragraphs []store.ParagraphRow) map[string]bool {
	out := make(map[string]bool, len(paragraphs))
	for _, pp := range paragraphs {
		out[pp.ID] = true
	}
	return out
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
			return nil, fmt.Errorf("%s cites unknown paragraph ID %q", recordType, pid)
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

func loadSummaryPrompt(p *project.Project, filename, fallbackVersion, fallbackPrompt string) (string, string) {
	path := p.Path(filepath.Join(project.PromptsDir, filename))
	data, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return fallbackPrompt, fallbackVersion
	}
	prompt := string(data)
	version := promptVersionFromText(prompt)
	if version == "" {
		version = fallbackVersion
	}
	return prompt, version
}

func promptVersionFromText(text string) string {
	const marker = "prompt_version:"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		version := strings.TrimSpace(line[idx+len(marker):])
		version = strings.TrimSuffix(version, "-->")
		return strings.TrimSpace(version)
	}
	return ""
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
func buildBookSummaryPrompt(title string, summaries []SummaryRecord, support []store.ParagraphRow) string {
	var sb strings.Builder
	sb.WriteString("Produce a whole-book orientation summary as evidence-backed JSON.\n")
	if title != "" {
		sb.WriteString("Book title: ")
		sb.WriteString(title)
		sb.WriteString("\n")
	}
	sb.WriteString("Return JSON matching the schema:\n")
	sb.WriteString(`{"summary":"...","themes":[],"unresolved":[],"evidence":["p-..."]}`)
	sb.WriteString("\nDo not cite only another summary; cite supporting paragraph IDs from the excerpts.\n\n")
	sb.WriteString("Chapter summaries:\n")
	for _, rec := range summaries {
		sb.WriteString("- ")
		sb.WriteString(rec.ChapterID)
		if rec.ChapterTitle != "" {
			sb.WriteString(" (")
			sb.WriteString(rec.ChapterTitle)
			sb.WriteString(")")
		}
		sb.WriteString(": ")
		sb.WriteString(rec.Summary)
		if len(rec.Evidence) > 0 {
			sb.WriteString(" Evidence: ")
			sb.WriteString(strings.Join(rec.Evidence, ", "))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nSupporting paragraphs:\n")
	for _, pp := range support {
		sb.WriteString("--- ")
		sb.WriteString(pp.ID)
		sb.WriteString(" ---\n")
		sb.WriteString(pp.Text)
		sb.WriteString("\n\n")
	}
	return sb.String()
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

func recordSummaryTask(run *Run, taskID, taskType, chapterID, status, errMsg string) {
	if run == nil {
		return
	}
	_ = run.recordTask(TaskRecord{
		TaskID:    taskID,
		RunID:     runID(run),
		TaskType:  taskType,
		ChapterID: chapterID,
		Status:    status,
		Error:     errMsg,
	})
}

const defaultChapterSummarySystemPrompt = `You are a literary analyst summarizing a manuscript chapter.
Return only valid JSON matching the requested schema. Do not add commentary outside the JSON object.
Cite only paragraph IDs that appear in the provided input.
Preserve uncertainty and do not resolve intentionally unresolved questions.`

const defaultBookSummarySystemPrompt = `You are a literary analyst producing a whole-book orientation summary.
Return only valid JSON matching the requested schema. Do not add commentary outside the JSON object.
Do not cite only chapter summaries; cite supporting paragraph IDs from the provided excerpts.
Preserve uncertainty and avoid unsupported conclusions.`
