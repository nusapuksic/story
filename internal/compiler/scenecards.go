package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/store"
)

// SceneCardRecord represents one scene card in model/scenes.jsonl.
type SceneCardRecord struct {
	RecordType   string              `json:"record_type"` // "scene_card"
	SceneID      string              `json:"scene_id"`
	Title        string              `json:"title"`
	Summary      string              `json:"summary"`
	POV          []string            `json:"pov,omitempty"`
	Participants []string            `json:"participants,omitempty"`
	Locations    []string            `json:"locations,omitempty"`
	Unresolved   []string            `json:"unresolved,omitempty"`
	Evidence     []string            `json:"evidence"`
	Generation   SceneCardGeneration `json:"generation"`
	Status       string              `json:"status"` // "generated"
}

// SceneCardGeneration is the provenance section of a scene card.
type SceneCardGeneration struct {
	RunID         string `json:"run_id"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
}

// rawSceneCard is the LLM-returned JSON before validation.
type rawSceneCard struct {
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	POV          []string `json:"pov"`
	Participants []string `json:"participants"`
	Locations    []string `json:"locations"`
	Unresolved   []string `json:"unresolved"`
	Evidence     []string `json:"evidence"`
}

// extractSceneCard calls the LLM extraction prompt for one scene, validates
// the response, and returns a SceneCardRecord.  If the LLM returns invalid
// data the function returns an error and a nil record.
func extractSceneCard(
	ctx context.Context,
	scene store.SceneRow,
	paragraphs []store.ParagraphRow,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
) (*SceneCardRecord, error) {
	if prov == nil {
		return nil, fmt.Errorf("no LLM provider: cannot extract scene card for %s", scene.ID)
	}

	// Build the evidence set – all paragraph IDs in the scene.
	pidSet := make(map[string]bool, len(paragraphs))
	for _, p := range paragraphs {
		pidSet[p.ID] = true
	}

	prompt := buildSceneCardPrompt(scene, paragraphs)
	taskID := ids.NewTaskID()
	req := provider.GenerationRequest{
		Model: model,
		Messages: []provider.Message{
			{Role: "system", Content: sceneExtractionSystemPrompt},
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
		t := TaskRecord{
			TaskID:   taskID,
			RunID:    runID(run),
			TaskType: "scene-extraction",
			SceneID:  scene.ID,
			Status:   TaskStatusFailed,
			Error:    err.Error(),
		}
		if run != nil {
			_ = run.recordTask(t)
		}
		return nil, fmt.Errorf("scene card LLM call for scene %s: %w", scene.ID, err)
	}

	card, parseErr := parseSceneCardResponse(resp.Content, scene.ID, pidSet, paragraphs, runID(run), model)
	status := TaskStatusCompleted
	errMsg := ""
	if parseErr != nil {
		status = TaskStatusFailed
		errMsg = parseErr.Error()
	}
	if run != nil {
		_ = run.recordTask(TaskRecord{
			TaskID:   taskID,
			RunID:    runID(run),
			TaskType: "scene-extraction",
			SceneID:  scene.ID,
			Status:   status,
			Error:    errMsg,
		})
	}
	return card, parseErr
}

// parseSceneCardResponse parses and validates the LLM response for scene card
// extraction.  It verifies that every evidence paragraph ID exists in pidSet.
func parseSceneCardResponse(
	content, sceneID string,
	pidSet map[string]bool,
	paragraphs []store.ParagraphRow,
	runID, model string,
) (*SceneCardRecord, error) {
	content = strings.TrimSpace(content)
	// Strip markdown code fences if present.
	if strings.HasPrefix(content, "```") {
		if i := strings.Index(content, "\n"); i >= 0 {
			content = content[i+1:]
		}
		if i := strings.LastIndex(content, "```"); i >= 0 {
			content = content[:i]
		}
		content = strings.TrimSpace(content)
	}

	var raw rawSceneCard
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, fmt.Errorf("parse scene card response for %s: %w", sceneID, err)
	}
	title := strings.TrimSpace(raw.Title)
	summary := strings.TrimSpace(raw.Summary)
	if summary == "" {
		summary = deriveSceneCardSummary(title, paragraphs, sceneID)
	}
	if title == "" {
		title = deriveSceneCardTitle(summary, sceneID)
	}
	// Validate evidence paragraph IDs.
	for _, pid := range raw.Evidence {
		if !pidSet[pid] {
			return nil, fmt.Errorf("scene card for %s: evidence cites unknown paragraph ID %q", sceneID, pid)
		}
	}

	return &SceneCardRecord{
		RecordType:   "scene_card",
		SceneID:      sceneID,
		Title:        title,
		Summary:      summary,
		POV:          raw.POV,
		Participants: raw.Participants,
		Locations:    raw.Locations,
		Unresolved:   raw.Unresolved,
		Evidence:     raw.Evidence,
		Generation: SceneCardGeneration{
			RunID:         runID,
			Model:         model,
			PromptVersion: "scene-extraction-v1",
		},
		Status: "generated",
	}, nil
}

func deriveSceneCardSummary(title string, paragraphs []store.ParagraphRow, sceneID string) string {
	if title = strings.TrimSpace(title); title != "" {
		return title
	}
	if summary := deriveSceneTextSummary(paragraphs); summary != "" {
		return summary
	}
	return fallbackSceneCardTitle(sceneID) + "."
}

func deriveSceneTextSummary(paragraphs []store.ParagraphRow) string {
	const maxSummaryRunes = 240

	for _, p := range paragraphs {
		text := strings.Join(strings.Fields(p.Text), " ")
		if text == "" {
			continue
		}
		if i := strings.IndexAny(text, ".!?"); i >= 0 {
			text = text[:i+1]
		}
		runes := []rune(text)
		if len(runes) > maxSummaryRunes {
			text = string(runes[:maxSummaryRunes])
			if i := strings.LastIndex(text, " "); i > 0 {
				text = text[:i]
			}
			text = strings.TrimSpace(text) + "..."
		}
		return text
	}
	return ""
}

func deriveSceneCardTitle(summary, sceneID string) string {
	const (
		maxTitleWords = 12
		maxTitleRunes = 80
	)

	words := strings.Fields(summary)
	if len(words) == 0 {
		return fallbackSceneCardTitle(sceneID)
	}
	if len(words) > maxTitleWords {
		words = words[:maxTitleWords]
	}

	title := trimDerivedTitle(strings.Join(words, " "))
	if len([]rune(title)) > maxTitleRunes {
		runes := []rune(title)
		title = string(runes[:maxTitleRunes])
		if i := strings.LastIndex(title, " "); i > 0 {
			title = title[:i]
		}
		title = trimDerivedTitle(title)
	}
	if title == "" {
		return fallbackSceneCardTitle(sceneID)
	}
	return title
}

func trimDerivedTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = strings.TrimRight(s, ".,;:!?-")
	return strings.TrimSpace(strings.Trim(s, `"'`))
}

func fallbackSceneCardTitle(sceneID string) string {
	return "Scene " + sceneID
}

// buildSceneCardPrompt constructs the user-turn message for scene card
// extraction.
func buildSceneCardPrompt(scene store.SceneRow, paragraphs []store.ParagraphRow) string {
	var sb strings.Builder
	sb.WriteString("Extract a structured scene card for this scene.\n")
	sb.WriteString("Scene ID: ")
	sb.WriteString(scene.ID)
	sb.WriteString("\n")
	sb.WriteString("Return JSON matching the schema:\n")
	sb.WriteString(`{"title":"...","summary":"...","pov":[],"participants":[],"locations":[],"unresolved":[],"evidence":["p-..."]}`)
	sb.WriteString("\n\nCite paragraph IDs for every concrete statement. Use only IDs from the list below.\n\n")
	for _, p := range paragraphs {
		sb.WriteString("--- ")
		sb.WriteString(p.ID)
		sb.WriteString(" ---\n")
		sb.WriteString(p.Text)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

const sceneExtractionSystemPrompt = `You are a literary analyst extracting structured scene cards from manuscript excerpts.
Return only valid JSON matching the requested schema. Do not add commentary outside the JSON object.
Cite only paragraph IDs that appear in the provided input.
Omit unsupported fields rather than guessing.`
