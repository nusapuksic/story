package compiler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/project"
	storyprompts "github.com/nusapuksic/story/internal/prompts"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/store"
)

// EntityRecord represents one candidate entity in model/entities.jsonl.
type EntityRecord struct {
	RecordType    string           `json:"record_type"`
	ID            string           `json:"id"`
	Type          string           `json:"type"`
	CanonicalName string           `json:"canonical_name"`
	Aliases       []string         `json:"aliases,omitempty"`
	Evidence      []string         `json:"evidence"`
	Generation    EntityGeneration `json:"generation"`
	Status        string           `json:"status"`
}

// MentionRecord represents one candidate entity mention in model/mentions.jsonl.
type MentionRecord struct {
	RecordType  string           `json:"record_type"`
	EntityID    string           `json:"entity_id"`
	ChapterID   string           `json:"chapter_id"`
	ParagraphID string           `json:"paragraph_id"`
	SurfaceText string           `json:"surface_text"`
	Confidence  float64          `json:"confidence"`
	Generation  EntityGeneration `json:"generation"`
	Status      string           `json:"status"`
}

// EntityGeneration is provenance for generated entity records.
type EntityGeneration struct {
	RunID         string `json:"run_id"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
	GeneratedAt   string `json:"generated_at"`
}

type rawEntityResponse struct {
	Entities []rawEntityCandidate `json:"entities"`
}

type rawEntityCandidate struct {
	CanonicalName flexibleString     `json:"canonical_name"`
	Type          flexibleString     `json:"type"`
	Aliases       flexibleStringList `json:"aliases"`
	Mentions      []rawMention       `json:"mentions"`
}

type rawMention struct {
	ParagraphID flexibleString `json:"paragraph_id"`
	SurfaceText flexibleString `json:"surface_text"`
	Confidence  float64        `json:"confidence"`
}

// compileEntities runs entity and mention extraction for requested chapters.
func compileEntities(
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
	entitiesFile, err := openAppendJSONL(p.Path(filepath.Join(project.ModelDir, "entities.jsonl")))
	if err != nil {
		return 0, err
	}
	defer entitiesFile.Close()

	mentionsFile, err := openAppendJSONL(p.Path(filepath.Join(project.ModelDir, "mentions.jsonl")))
	if err != nil {
		return 0, err
	}
	defer mentionsFile.Close()

	summaryContext, err := readSummaryIndex(p.Path(filepath.Join(project.ModelDir, "summaries.jsonl")))
	if err != nil {
		return 0, fmt.Errorf("read summaries for entity context: %w", err)
	}

	items := make([]OrderedWorkItem[entityWorkInput], 0, len(chapters))
	for chapterIndex, ch := range chapters {
		if !opts.Force {
			n, err := st.EntityMentionCountByChapter(ch.ID)
			if err != nil {
				return 0, err
			}
			if n > 0 {
				reportProgress(opts, ProgressEvent{Layer: LayerEntities, Stage: "item-skip", ChapterID: ch.ID, Current: chapterIndex + 1, Total: len(chapters), Message: fmt.Sprintf("Entities %s (%d/%d): already exists", ch.ID, chapterIndex+1, len(chapters))})
				continue
			}
		}

		paragraphs, err := st.ParagraphsByChapter(ch.ID)
		if err != nil {
			return 0, err
		}
		if len(paragraphs) == 0 {
			reportProgress(opts, ProgressEvent{Layer: LayerEntities, Stage: "item-skip", ChapterID: ch.ID, Current: chapterIndex + 1, Total: len(chapters), Message: fmt.Sprintf("Entities %s (%d/%d): no paragraphs", ch.ID, chapterIndex+1, len(chapters))})
			continue
		}

		promptContext, err := entityExtractionContextForChapter(st, ch, summaryContext)
		if err != nil {
			return 0, err
		}

		items = append(items, OrderedWorkItem[entityWorkInput]{
			Sequence: len(items),
			TaskID:   ch.ID,
			Input: entityWorkInput{
				Chapter:       ch,
				ChapterIndex:  chapterIndex,
				ChapterTotal:  len(chapters),
				Paragraphs:    paragraphs,
				PromptContext: promptContext,
				Force:         opts.Force,
			},
		})
	}

	total := 0
	err = RunOrderedWork(ctx, items, OrderedExecutorOptions{WorkerLimit: 1}, func(ctx context.Context, item OrderedWorkItem[entityWorkInput]) (entityWorkOutput, error) {
		input := item.Input
		candidates, err := extractEntitiesForChapter(ctx, p, input.Chapter, input.Paragraphs, input.PromptContext,
			opts.ExtractionProvider, opts.ExtractionModel, cfg, run)
		if err != nil {
			return entityWorkOutput{}, err
		}
		output := entityWorkOutput{Input: input, Candidates: candidates}
		if staging != nil {
			ref, err := stageEntityWorkResult(staging, item.Sequence, output)
			if err != nil {
				return entityWorkOutput{}, err
			}
			output.Staged = ref
		}
		return output, nil
	}, func(ctx context.Context, result OrderedWorkResult[entityWorkOutput]) error {
		output := result.Output
		input := output.Input
		reportProgress(opts, ProgressEvent{Layer: LayerEntities, Stage: "item-start", ChapterID: input.Chapter.ID, Current: input.ChapterIndex + 1, Total: input.ChapterTotal, Message: fmt.Sprintf("Entities %s (%d/%d): extracting from %d paragraph(s)", input.Chapter.ID, input.ChapterIndex+1, input.ChapterTotal, len(input.Paragraphs))})
		if input.Force {
			if err := st.DeleteEntityMentionsForChapter(input.Chapter.ID); err != nil {
				return err
			}
		}
		entities, mentions := finalizeEntityCandidates(output.Candidates)
		for _, entity := range entities {
			if err := st.InsertEntity(entityRowFromRecord(entity)); err != nil {
				return err
			}
			if err := appendJSONL(entitiesFile, entity); err != nil {
				return err
			}
			total++
		}
		for _, mention := range mentions {
			if err := st.InsertMention(mentionRowFromRecord(mention)); err != nil {
				return err
			}
			if err := appendJSONL(mentionsFile, mention); err != nil {
				return err
			}
		}
		if staging != nil {
			if err := staging.RecordCommit(output.Staged); err != nil {
				return err
			}
		}
		reportProgress(opts, ProgressEvent{Layer: LayerEntities, Stage: "item-complete", ChapterID: input.Chapter.ID, Current: input.ChapterIndex + 1, Total: input.ChapterTotal, Message: fmt.Sprintf("Entities %s (%d/%d): completed (%d entities, %d mentions)", input.Chapter.ID, input.ChapterIndex+1, input.ChapterTotal, len(entities), len(mentions))})
		return nil
	})
	if err != nil {
		return total, err
	}
	return total, nil
}

func stageEntityWorkResult(staging *RunStagingStore, sequence int, output entityWorkOutput) (StagedResultRef, error) {
	if staging == nil {
		return StagedResultRef{}, nil
	}
	return staging.StageJSON(LayerEntities, StagedResultMeta{
		Sequence:      sequence,
		TaskID:        output.Input.Chapter.ID,
		TargetID:      output.Input.Chapter.ID,
		SchemaVersion: 1,
	}, stagedEntityPayload{Candidates: output.Candidates})
}

func finalizeEntityCandidates(candidates []entityRecordCandidate) ([]EntityRecord, []MentionRecord) {
	entities := make([]EntityRecord, 0, len(candidates))
	var mentions []MentionRecord
	for _, candidate := range candidates {
		entityID := ids.NewEntityID()
		entity := EntityRecord{
			RecordType:    "entity",
			ID:            entityID,
			Type:          candidate.Type,
			CanonicalName: candidate.CanonicalName,
			Aliases:       candidate.Aliases,
			Evidence:      candidate.Evidence,
			Generation:    candidate.Generation,
			Status:        candidate.Status,
		}
		entities = append(entities, entity)
		for _, mentionCandidate := range candidate.Mentions {
			mentions = append(mentions, MentionRecord{
				RecordType:  "mention",
				EntityID:    entityID,
				ChapterID:   mentionCandidate.ChapterID,
				ParagraphID: mentionCandidate.ParagraphID,
				SurfaceText: mentionCandidate.SurfaceText,
				Confidence:  mentionCandidate.Confidence,
				Generation:  mentionCandidate.Generation,
				Status:      mentionCandidate.Status,
			})
		}
	}
	return entities, mentions
}

func entityRowFromRecord(entity EntityRecord) store.EntityRow {
	rawBytes, _ := json.Marshal(entity)
	return store.EntityRow{
		ID:              entity.ID,
		Type:            entity.Type,
		CanonicalName:   entity.CanonicalName,
		Aliases:         entity.Aliases,
		Evidence:        entity.Evidence,
		GenerationRun:   entity.Generation.RunID,
		GenerationModel: entity.Generation.Model,
		PromptVersion:   entity.Generation.PromptVersion,
		Status:          entity.Status,
		RawJSON:         string(rawBytes),
	}
}

func mentionRowFromRecord(mention MentionRecord) store.MentionRow {
	rawBytes, _ := json.Marshal(mention)
	return store.MentionRow{
		EntityID:        mention.EntityID,
		ChapterID:       mention.ChapterID,
		ParagraphID:     mention.ParagraphID,
		SurfaceText:     mention.SurfaceText,
		Confidence:      mention.Confidence,
		GenerationRun:   mention.Generation.RunID,
		GenerationModel: mention.Generation.Model,
		PromptVersion:   mention.Generation.PromptVersion,
		Status:          mention.Status,
		RawJSON:         string(rawBytes),
	}
}
func extractEntitiesForChapter(
	ctx context.Context,
	p *project.Project,
	ch store.ChapterRow,
	paragraphs []store.ParagraphRow,
	promptContext entityExtractionContext,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
) ([]entityRecordCandidate, error) {
	if prov == nil {
		return nil, fmt.Errorf("no LLM provider: cannot extract entities for %s", ch.ID)
	}
	loadedPrompt := loadCompilerPrompt(p, storyprompts.EntityResolution)
	prompt := buildEntityPrompt(ch, paragraphs, promptContext)
	taskID := ids.NewTaskID()
	req := provider.GenerationRequest{
		Model: model,
		Messages: []provider.Message{
			{Role: "system", Content: loadedPrompt.Content},
			{Role: "user", Content: prompt},
		},
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxOutputTokens,
		JSONMode:    true,
	}
	resp, timing, err := generateWithAudit(ctx, run, taskID, prov, req)
	if err != nil {
		recordEntityTask(run, taskID, ch.ID, TaskStatusFailed, loadedPrompt.Version, err.Error(), timing)
		return nil, fmt.Errorf("entity extraction LLM call for %s: %w", ch.ID, err)
	}

	candidates, parseErr := parseEntityResponse(resp.Content, ch.ID, paragraphs, runID(run), model, loadedPrompt.Version)
	status := TaskStatusCompleted
	errMsg := ""
	if parseErr != nil {
		status = TaskStatusFailed
		errMsg = parseErr.Error()
	}
	recordEntityTask(run, taskID, ch.ID, status, loadedPrompt.Version, errMsg, timing)
	return candidates, parseErr
}

type entityExtractionContext struct {
	BookSummary    *SummaryRecord
	ChapterSummary *SummaryRecord
	SceneCards     []SceneCardRecord
}

func entityExtractionContextForChapter(st *store.Store, ch store.ChapterRow, summaries summaryIndex) (entityExtractionContext, error) {
	var out entityExtractionContext
	if summaries.Book != nil && strings.TrimSpace(summaries.Book.Summary) != "" {
		rec := *summaries.Book
		out.BookSummary = &rec
	}
	if rec, ok := summaries.Chapters[ch.ID]; ok && strings.TrimSpace(rec.Summary) != "" {
		rec := rec
		out.ChapterSummary = &rec
	}

	scenes, err := st.ScenesByChapter(ch.ID)
	if err != nil {
		return out, err
	}
	for _, sc := range scenes {
		card, err := st.InspectSceneCard(sc.ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return out, err
		}
		rec := sceneCardRecordFromRow(card)
		if strings.TrimSpace(rec.Title) == "" && strings.TrimSpace(rec.Summary) == "" {
			continue
		}
		out.SceneCards = append(out.SceneCards, rec)
	}
	return out, nil
}

func (c entityExtractionContext) hasContent() bool {
	return c.BookSummary != nil || c.ChapterSummary != nil || len(c.SceneCards) > 0
}

func writeEntityExtractionContext(sb *strings.Builder, promptContext entityExtractionContext) {
	if !promptContext.hasContent() {
		return
	}

	sb.WriteString("\nExisting extraction context (orientation only; do not cite this section):\n")
	writeEntitySummaryContext(sb, "Book summary", promptContext.BookSummary)
	writeEntitySummaryContext(sb, "Chapter summary", promptContext.ChapterSummary)
	if len(promptContext.SceneCards) > 0 {
		sb.WriteString("Scene cards:\n")
		for _, card := range promptContext.SceneCards {
			writeEntitySceneCardContext(sb, card)
		}
	}
}

func writeEntitySummaryContext(sb *strings.Builder, label string, rec *SummaryRecord) {
	if rec == nil {
		return
	}
	summary := cleanEntityContextText(rec.Summary)
	if summary == "" && len(rec.Themes) == 0 && len(rec.Unresolved) == 0 {
		return
	}
	sb.WriteString(label)
	sb.WriteString(":")
	if summary != "" {
		sb.WriteString(" ")
		sb.WriteString(summary)
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

func writeEntitySceneCardContext(sb *strings.Builder, card SceneCardRecord) {
	sb.WriteString("- ")
	if card.SceneID != "" {
		sb.WriteString(card.SceneID)
		sb.WriteString(": ")
	}
	if title := strings.TrimSpace(card.Title); title != "" {
		sb.WriteString(title)
	}
	if summary := cleanEntityContextText(card.Summary); summary != "" {
		if strings.TrimSpace(card.Title) != "" {
			sb.WriteString(" - ")
		}
		sb.WriteString(summary)
	}
	writeEntityContextList(sb, "POV", card.POV)
	writeEntityContextList(sb, "Participants", card.Participants)
	writeEntityContextList(sb, "Locations", card.Locations)
	writeEntityContextList(sb, "Unresolved", card.Unresolved)
	writeEntityContextList(sb, "Card evidence IDs", card.Evidence)
	sb.WriteString("\n")
}

func writeEntityContextList(sb *strings.Builder, label string, values []string) {
	values = dedupeStrings(values)
	if len(values) == 0 {
		return
	}
	sb.WriteString(" ")
	sb.WriteString(label)
	sb.WriteString(": ")
	sb.WriteString(strings.Join(values, ", "))
	sb.WriteString(".")
}

func cleanEntityContextText(text string) string {
	return strings.TrimSpace(chapterSummaryTextForBookPrompt(text))
}

func buildEntityPrompt(ch store.ChapterRow, paragraphs []store.ParagraphRow, promptContext entityExtractionContext) string {
	var sb strings.Builder
	sb.WriteString("Extract candidate entities and textual mentions from this chapter as JSON.\n")
	sb.WriteString("Chapter ID: ")
	sb.WriteString(ch.ID)
	sb.WriteString("\nTitle: ")
	sb.WriteString(ch.Title)
	sb.WriteString("\nReturn JSON matching the schema:\n")
	sb.WriteString(`{"entities":[{"canonical_name":"...","type":"character|location|object|organization|group|document|event-concept|unknown","aliases":[],"mentions":[{"paragraph_id":"p-...","surface_text":"...","confidence":0.9}]}]}`)
	sb.WriteString("\nUse only paragraph IDs from the excerpts below. Preserve ambiguity; do not merge uncertain aliases.")
	sb.WriteString(" Existing extraction context, when present, is orientation only and not evidence.\n")
	writeEntityExtractionContext(&sb, promptContext)
	sb.WriteString("\nAuthoritative manuscript paragraph excerpts:\n")
	writeParagraphExcerpts(&sb, paragraphs)
	return sb.String()
}

func parseEntityResponse(content, chapterID string, paragraphs []store.ParagraphRow, runID, model, promptVersion string) ([]entityRecordCandidate, error) {
	content = stripJSONFences(content)
	var raw rawEntityResponse
	strictEvidence := true
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		if !isTruncatedJSONError(err) {
			return nil, fmt.Errorf("parse entity response for %s: %w", chapterID, err)
		}
		raw = salvageTruncatedEntityResponse(content)
		strictEvidence = false
	}
	return entityCandidatesFromRaw(raw, chapterID, paragraphs, runID, model, promptVersion, strictEvidence)
}

func entityCandidatesFromRaw(
	raw rawEntityResponse,
	chapterID string,
	paragraphs []store.ParagraphRow,
	runID, model, promptVersion string,
	strictEvidence bool,
) ([]entityRecordCandidate, error) {
	paragraphByID := make(map[string]store.ParagraphRow, len(paragraphs))
	for _, pp := range paragraphs {
		paragraphByID[pp.ID] = pp
	}

	generation := EntityGeneration{
		RunID:         runID,
		Model:         model,
		PromptVersion: promptVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	var candidates []entityRecordCandidate
	for _, cand := range raw.Entities {
		name := strings.TrimSpace(string(cand.CanonicalName))
		if name == "" {
			continue
		}
		evidence := make([]string, 0, len(cand.Mentions))
		seenEvidence := make(map[string]bool)
		var mentions []mentionRecordCandidate
		for _, rawMention := range cand.Mentions {
			pid := strings.TrimSpace(string(rawMention.ParagraphID))
			pp, ok := paragraphByID[pid]
			if !ok {
				if strictEvidence {
					return nil, fmt.Errorf("entity %q cites unknown paragraph ID %q", name, pid)
				}
				continue
			}
			surface := strings.TrimSpace(string(rawMention.SurfaceText))
			if surface == "" {
				surface = name
			}
			confidence := rawMention.Confidence
			if confidence < 0 {
				confidence = 0
			}
			if confidence > 1 {
				confidence = 1
			}
			mentions = append(mentions, mentionRecordCandidate{
				ChapterID:   pp.ChapterID,
				ParagraphID: pid,
				SurfaceText: surface,
				Confidence:  confidence,
				Generation:  generation,
				Status:      "generated",
			})
			if !seenEvidence[pid] {
				seenEvidence[pid] = true
				evidence = append(evidence, pid)
			}
		}
		if len(mentions) == 0 {
			continue
		}
		candidates = append(candidates, entityRecordCandidate{
			Type:          normalizeEntityType(string(cand.Type)),
			CanonicalName: name,
			Aliases:       dedupeStrings([]string(cand.Aliases)),
			Evidence:      evidence,
			Mentions:      mentions,
			Generation:    generation,
			Status:        "generated",
		})
	}
	return candidates, nil
}
func salvageTruncatedEntityResponse(content string) rawEntityResponse {
	arrayStart, ok := entityArrayStart(content)
	if !ok {
		return rawEntityResponse{}
	}

	var out rawEntityResponse
	for _, snippet := range completeJSONObjectSnippets(content, arrayStart) {
		var cand rawEntityCandidate
		if err := json.Unmarshal([]byte(snippet), &cand); err == nil {
			out.Entities = append(out.Entities, cand)
		}
	}
	return out
}

func entityArrayStart(content string) (int, bool) {
	key := strings.Index(content, `"entities"`)
	if key < 0 {
		return -1, false
	}
	for i := key + len(`"entities"`); i < len(content); i++ {
		switch content[i] {
		case '[':
			return i, true
		case ':', ' ', '\n', '\r', '\t':
			continue
		default:
			return -1, false
		}
	}
	return -1, false
}

func completeJSONObjectSnippets(content string, arrayStart int) []string {
	var snippets []string
	inString := false
	escaped := false
	depth := 0
	objectStart := -1

	for i := arrayStart + 1; i < len(content); i++ {
		ch := content[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case ']':
			if objectStart < 0 {
				return snippets
			}
			depth--
		case '{':
			if objectStart < 0 {
				objectStart = i
			}
			depth++
		case '[':
			if objectStart >= 0 {
				depth++
			}
		case '}':
			if objectStart >= 0 {
				depth--
				if depth == 0 {
					snippets = append(snippets, content[objectStart:i+1])
					objectStart = -1
				}
			}
		}
	}
	return snippets
}

func normalizeEntityType(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "character", "location", "object", "organization", "group", "document", "event-concept", "unknown":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "unknown"
	}
}

func recordEntityTask(run *Run, taskID, chapterID, status, promptVersion, errMsg string, timings ...taskTiming) {
	if run == nil {
		return
	}
	record := TaskRecord{
		TaskID:        taskID,
		RunID:         runID(run),
		TaskType:      "entity-resolution",
		ChapterID:     chapterID,
		PromptVersion: promptVersion,
		Status:        status,
		Error:         errMsg,
	}
	if len(timings) > 0 {
		timings[0].applyTo(&record)
	}
	_ = run.recordTask(record)
}
