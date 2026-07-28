package compiler

import (
	"context"
	"encoding/json"
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

	total := 0
	for _, ch := range chapters {
		if opts.Force {
			if err := st.DeleteEntityMentionsForChapter(ch.ID); err != nil {
				return total, err
			}
		} else {
			n, err := st.EntityMentionCountByChapter(ch.ID)
			if err != nil {
				return total, err
			}
			if n > 0 {
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

		entities, mentions, err := extractEntitiesForChapter(ctx, p, ch, paragraphs,
			opts.ExtractionProvider, opts.ExtractionModel, cfg, run)
		if err != nil {
			return total, err
		}

		for _, entity := range entities {
			rawBytes, _ := json.Marshal(entity)
			row := store.EntityRow{
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
			if err := st.InsertEntity(row); err != nil {
				return total, err
			}
			if err := appendJSONL(entitiesFile, entity); err != nil {
				return total, err
			}
			total++
		}
		for _, mention := range mentions {
			rawBytes, _ := json.Marshal(mention)
			row := store.MentionRow{
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
			if err := st.InsertMention(row); err != nil {
				return total, err
			}
			if err := appendJSONL(mentionsFile, mention); err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

func extractEntitiesForChapter(
	ctx context.Context,
	p *project.Project,
	ch store.ChapterRow,
	paragraphs []store.ParagraphRow,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
) ([]EntityRecord, []MentionRecord, error) {
	if prov == nil {
		return nil, nil, fmt.Errorf("no LLM provider: cannot extract entities for %s", ch.ID)
	}
	loadedPrompt := loadCompilerPrompt(p, storyprompts.EntityResolution)
	prompt := buildEntityPrompt(ch, paragraphs)
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
	resp, err := prov.Generate(ctx, req)
	if run != nil {
		_ = run.saveRawResponse(taskID, resp.Content)
	}
	if err != nil {
		recordEntityTask(run, taskID, ch.ID, TaskStatusFailed, loadedPrompt.Version, err.Error())
		return nil, nil, fmt.Errorf("entity extraction LLM call for %s: %w", ch.ID, err)
	}

	entities, mentions, parseErr := parseEntityResponse(resp.Content, ch.ID, paragraphs, runID(run), model, loadedPrompt.Version)
	status := TaskStatusCompleted
	errMsg := ""
	if parseErr != nil {
		status = TaskStatusFailed
		errMsg = parseErr.Error()
	}
	recordEntityTask(run, taskID, ch.ID, status, loadedPrompt.Version, errMsg)
	return entities, mentions, parseErr
}

func buildEntityPrompt(ch store.ChapterRow, paragraphs []store.ParagraphRow) string {
	var sb strings.Builder
	sb.WriteString("Extract candidate entities and textual mentions from this chapter as JSON.\n")
	sb.WriteString("Chapter ID: ")
	sb.WriteString(ch.ID)
	sb.WriteString("\nTitle: ")
	sb.WriteString(ch.Title)
	sb.WriteString("\nReturn JSON matching the schema:\n")
	sb.WriteString(`{"entities":[{"canonical_name":"...","type":"character|location|object|organization|group|document|event-concept|unknown","aliases":[],"mentions":[{"paragraph_id":"p-...","surface_text":"...","confidence":0.9}]}]}`)
	sb.WriteString("\nUse only paragraph IDs from the excerpts below. Preserve ambiguity; do not merge uncertain aliases.\n\n")
	writeParagraphExcerpts(&sb, paragraphs)
	return sb.String()
}

func parseEntityResponse(content, chapterID string, paragraphs []store.ParagraphRow, runID, model, promptVersion string) ([]EntityRecord, []MentionRecord, error) {
	content = stripJSONFences(content)
	var raw rawEntityResponse
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, nil, fmt.Errorf("parse entity response for %s: %w", chapterID, err)
	}

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
	var entities []EntityRecord
	var mentions []MentionRecord
	for _, cand := range raw.Entities {
		name := strings.TrimSpace(string(cand.CanonicalName))
		if name == "" {
			continue
		}
		entityID := ids.NewEntityID()
		evidence := make([]string, 0, len(cand.Mentions))
		seenEvidence := make(map[string]bool)
		var entityMentions []MentionRecord
		for _, rawMention := range cand.Mentions {
			pid := strings.TrimSpace(string(rawMention.ParagraphID))
			pp, ok := paragraphByID[pid]
			if !ok {
				return nil, nil, fmt.Errorf("entity %q cites unknown paragraph ID %q", name, pid)
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
			entityMentions = append(entityMentions, MentionRecord{
				RecordType:  "mention",
				EntityID:    entityID,
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
		if len(entityMentions) == 0 {
			continue
		}
		entities = append(entities, EntityRecord{
			RecordType:    "entity",
			ID:            entityID,
			Type:          normalizeEntityType(string(cand.Type)),
			CanonicalName: name,
			Aliases:       dedupeStrings([]string(cand.Aliases)),
			Evidence:      evidence,
			Generation:    generation,
			Status:        "generated",
		})
		mentions = append(mentions, entityMentions...)
	}
	return entities, mentions, nil
}

func normalizeEntityType(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "character", "location", "object", "organization", "group", "document", "event-concept", "unknown":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "unknown"
	}
}

func recordEntityTask(run *Run, taskID, chapterID, status, promptVersion, errMsg string) {
	if run == nil {
		return
	}
	_ = run.recordTask(TaskRecord{
		TaskID:        taskID,
		RunID:         runID(run),
		TaskType:      "entity-resolution",
		ChapterID:     chapterID,
		PromptVersion: promptVersion,
		Status:        status,
		Error:         errMsg,
	})
}
