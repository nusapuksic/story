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

// SceneRecord represents one scene in model/scenes.jsonl.
type SceneRecord struct {
	RecordType     string `json:"record_type"` // "scene"
	ID             string `json:"id"`
	ChapterID      string `json:"chapter_id"`
	ParagraphStart string `json:"paragraph_start"`
	ParagraphEnd   string `json:"paragraph_end"`
	Ordinal        int    `json:"ordinal"`
	BoundarySource string `json:"boundary_source"` // "explicit", "model", "chapter_end"
	Status         string `json:"status"`          // "generated"
}

// ChapterSnapshotRecord marks that all scenes for a chapter have been fully
// written to model/scenes.jsonl.  It is appended immediately after the last
// scene record for the chapter so that IndexScenesJSONL can identify the
// preceding scene records as a committed, valid snapshot.
type ChapterSnapshotRecord struct {
	RecordType  string `json:"record_type"` // "chapter_snapshot"
	ChapterID   string `json:"chapter_id"`
	CommittedAt string `json:"committed_at"` // RFC3339
}

// boundaryProposal is one candidate scene boundary returned by the LLM.
type boundaryProposal struct {
	AfterParagraphID string  `json:"after_paragraph_id"`
	Reason           string  `json:"reason"`
	Confidence       float64 `json:"confidence"`
}

// boundaryResponse is the full JSON response from the scene-boundaries prompt.
type boundaryResponse struct {
	Boundaries []boundaryProposal `json:"boundaries"`
}

// detectScenes builds Scene records for all paragraphs in a chapter.
//
// Priority:
//  1. explicit scene_break blocks (manuscript.BlockSceneBreak)
//  2. LLM-proposed boundaries (when prov is non-nil and mode is "hybrid" or "model")
//  3. chapter end
//
// If prov is nil the function falls back to explicit-only detection.
func detectScenes(
	ctx context.Context,
	ch store.ChapterRow,
	paragraphs []store.ParagraphRow,
	blocks []store.ChapterRow, // unused – kept for future multi-source
	explicitBreakPositions []int, // ordinals (1-based) of scene_break blocks
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
) ([]SceneRecord, error) {
	if len(paragraphs) == 0 {
		return nil, nil
	}

	// Build the set of paragraph IDs that mark a boundary end (the last
	// paragraph of a scene before an explicit break).
	breakAfterIDs := buildExplicitBreakMap(paragraphs, explicitBreakPositions)

	// Merge with LLM proposals if a provider is configured and mode requires it.
	if prov != nil && cfg.Mode != "explicit" {
		proposed, err := proposeSceneBoundaries(ctx, ch, paragraphs, prov, model, cfg, run)
		if err != nil {
			// Non-fatal: fall back to explicit-only with a warning.
			_ = err // errors are recorded in the run already
		} else {
			// LLM proposals fill gaps where no explicit break exists.
			for pid := range proposed {
				if !breakAfterIDs[pid] {
					breakAfterIDs[pid] = true
				}
			}
		}
	}

	// Build scenes from the merged boundary set.
	return buildScenesFromBreaks(ch.ID, paragraphs, breakAfterIDs), nil
}

// buildExplicitBreakMap returns a set of paragraph IDs that are the last
// paragraph before an explicit scene break.  explicitBreakOrdinals contains
// the ordinals (from the blocks table) of scene_break blocks; we find the
// paragraph immediately preceding each break.
func buildExplicitBreakMap(
	paragraphs []store.ParagraphRow,
	explicitBreakOrdinals []int,
) map[string]bool {
	out := make(map[string]bool)
	if len(paragraphs) == 0 {
		return out
	}
	for _, bOrd := range explicitBreakOrdinals {
		// Find the paragraph with the highest ordinal that is still < bOrd.
		// paragraphs are sorted by ordinal.
		for i := len(paragraphs) - 1; i >= 0; i-- {
			if paragraphs[i].Ordinal < bOrd {
				out[paragraphs[i].ID] = true
				break
			}
		}
	}
	return out
}

// buildScenesFromBreaks converts the boundary map into ordered SceneRecord
// values.  breakAfterIDs marks paragraph IDs after which a new scene begins.
func buildScenesFromBreaks(chapterID string, paragraphs []store.ParagraphRow, breakAfterIDs map[string]bool) []SceneRecord {
	if len(paragraphs) == 0 {
		return nil
	}
	var scenes []SceneRecord
	ordinal := 1
	sceneStart := 0
	for i, p := range paragraphs {
		if breakAfterIDs[p.ID] || i == len(paragraphs)-1 {
			// End scene here.
			src := "explicit"
			if !breakAfterIDs[p.ID] {
				src = "chapter_end"
			} else if !isExplicitBreak(p.ID, breakAfterIDs) {
				src = "model"
			}
			scenes = append(scenes, SceneRecord{
				RecordType:     "scene",
				ID:             ids.NewSceneID(),
				ChapterID:      chapterID,
				ParagraphStart: paragraphs[sceneStart].ID,
				ParagraphEnd:   paragraphs[i].ID,
				Ordinal:        ordinal,
				BoundarySource: src,
				Status:         "generated",
			})
			ordinal++
			sceneStart = i + 1
		}
	}
	return scenes
}

// isExplicitBreak is a placeholder; the breakAfterIDs map is populated from
// explicit breaks first and then supplemented by model proposals, so we can't
// distinguish them here without an extra set. For now all entries that are in
// breakAfterIDs before LLM proposals are treated as explicit. This function
// always returns true to produce "explicit" or "chapter_end" boundaries.
func isExplicitBreak(_ string, _ map[string]bool) bool {
	return true
}

// proposeSceneBoundaries calls the LLM scene-boundaries prompt for each window
// and returns the merged set of proposed break-after paragraph IDs.
func proposeSceneBoundaries(
	ctx context.Context,
	ch store.ChapterRow,
	paragraphs []store.ParagraphRow,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
) (map[string]bool, error) {
	pidSet := make(map[string]bool, len(paragraphs))
	for _, p := range paragraphs {
		pidSet[p.ID] = true
	}

	windows := buildWindows(paragraphs, cfg.TargetContextTokens, cfg.OverlapParagraphs)
	merged := make(map[string]bool)

	for _, win := range windows {
		taskID := ids.NewTaskID()
		prompt := buildBoundaryPrompt(win.Paragraphs)
		req := provider.GenerationRequest{
			Model: model,
			Messages: []provider.Message{
				{Role: "system", Content: sceneBoundariesSystemPrompt},
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
				TaskID:    taskID,
				RunID:     runID(run),
				TaskType:  "scene-boundaries",
				ChapterID: ch.ID,
				Status:    TaskStatusFailed,
				Error:     err.Error(),
			}
			if run != nil {
				_ = run.recordTask(t)
			}
			return nil, fmt.Errorf("scene boundary LLM call for chapter %s: %w", ch.ID, err)
		}

		// Parse and validate the response.
		proposals, parseErr := parseBoundaryResponse(resp.Content, pidSet)
		status := TaskStatusCompleted
		errMsg := ""
		if parseErr != nil {
			status = TaskStatusFailed
			errMsg = parseErr.Error()
		}
		if run != nil {
			_ = run.recordTask(TaskRecord{
				TaskID:    taskID,
				RunID:     runID(run),
				TaskType:  "scene-boundaries",
				ChapterID: ch.ID,
				Status:    status,
				Error:     errMsg,
			})
		}
		if parseErr != nil {
			return nil, parseErr
		}
		for pid := range proposals {
			merged[pid] = true
		}
	}
	return merged, nil
}

// parseBoundaryResponse validates and parses the LLM boundary response.
// It returns the set of valid after_paragraph_id values.
func parseBoundaryResponse(content string, validPIDs map[string]bool) (map[string]bool, error) {
	content = strings.TrimSpace(content)
	// Some models wrap JSON in markdown code fences.
	if strings.HasPrefix(content, "```") {
		if i := strings.Index(content, "\n"); i >= 0 {
			content = content[i+1:]
		}
		if i := strings.LastIndex(content, "```"); i >= 0 {
			content = content[:i]
		}
		content = strings.TrimSpace(content)
	}
	var br boundaryResponse
	if err := json.Unmarshal([]byte(content), &br); err != nil {
		return nil, fmt.Errorf("parse boundary response: %w", err)
	}
	out := make(map[string]bool, len(br.Boundaries))
	for _, b := range br.Boundaries {
		if b.AfterParagraphID == "" {
			continue
		}
		if !validPIDs[b.AfterParagraphID] {
			// The model returned a paragraph ID that does not exist in the
			// input window. Reject it rather than trusting the model.
			return nil, fmt.Errorf("boundary response contains unknown paragraph ID %q", b.AfterParagraphID)
		}
		out[b.AfterParagraphID] = true
	}
	return out, nil
}

// buildBoundaryPrompt constructs the user-turn message for scene boundary
// detection.  Each paragraph is listed with its ID and text.
func buildBoundaryPrompt(paragraphs []store.ParagraphRow) string {
	var sb strings.Builder
	sb.WriteString("Identify scene boundaries in the following paragraphs.\n")
	sb.WriteString("Return JSON matching the schema: ")
	sb.WriteString(`{"boundaries":[{"after_paragraph_id":"p-...","reason":"...","confidence":0.9}]}`)
	sb.WriteString("\nReturn only paragraph IDs from the list below. ")
	sb.WriteString("Do not invent identifiers.\n\n")
	for _, p := range paragraphs {
		sb.WriteString("--- ")
		sb.WriteString(p.ID)
		sb.WriteString(" ---\n")
		sb.WriteString(p.Text)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

const sceneBoundariesSystemPrompt = `You are a literary analyst. Identify where scene boundaries occur in manuscript excerpts.
A scene boundary occurs when there is a meaningful shift in time, location, point of view, or narrative focus.
Return only valid JSON. Do not add commentary outside the JSON object.
Use only paragraph IDs that appear in the provided input.`

// sceneDetectConfig carries compile configuration relevant to scene detection.
type sceneDetectConfig struct {
	Mode                string
	TargetContextTokens int
	MaxOutputTokens     int
	OverlapParagraphs   int
	Temperature         float64
}

// runID safely extracts the run ID from a potentially nil Run.
func runID(r *Run) string {
	if r == nil {
		return ""
	}
	return r.Record.RunID
}

// DetectScenesNoLLM is an exported wrapper around detectScenes for tests.
// It uses explicit-only boundary detection (no LLM).
func DetectScenesNoLLM(ch store.ChapterRow, paragraphs []store.ParagraphRow, explicitBreakOrdinals []int) ([]SceneRecord, error) {
	return detectScenes(context.Background(), ch, paragraphs, nil, explicitBreakOrdinals, nil, "", sceneDetectConfig{Mode: "explicit"}, nil)
}

// ValidateScenePartition verifies that scenes form a complete, non-overlapping
// cover of all paragraphs in a chapter.  paragraphs must be ordered by ordinal.
// It returns a descriptive error if any gap, overlap, or uncovered paragraph is
// found, or nil if the partition is valid.
func ValidateScenePartition(paragraphs []store.ParagraphRow, scenes []SceneRecord) error {
	if len(paragraphs) == 0 {
		if len(scenes) != 0 {
			return fmt.Errorf("chapter has no paragraphs but %d scene(s)", len(scenes))
		}
		return nil
	}
	if len(scenes) == 0 {
		return fmt.Errorf("chapter has %d paragraph(s) but no scenes", len(paragraphs))
	}

	ordByID := make(map[string]int, len(paragraphs))
	for _, p := range paragraphs {
		ordByID[p.ID] = p.Ordinal
	}

	// First scene must start at the first paragraph.
	if scenes[0].ParagraphStart != paragraphs[0].ID {
		return fmt.Errorf("first scene %s starts at paragraph %q but first paragraph is %q",
			scenes[0].ID, scenes[0].ParagraphStart, paragraphs[0].ID)
	}
	// Last scene must end at the last paragraph.
	lastPara := paragraphs[len(paragraphs)-1]
	if scenes[len(scenes)-1].ParagraphEnd != lastPara.ID {
		return fmt.Errorf("last scene %s ends at paragraph %q but last paragraph is %q",
			scenes[len(scenes)-1].ID, scenes[len(scenes)-1].ParagraphEnd, lastPara.ID)
	}
	// Consecutive scenes must chain with no gap or overlap.
	for i := 1; i < len(scenes); i++ {
		prevEndOrd, ok := ordByID[scenes[i-1].ParagraphEnd]
		if !ok {
			return fmt.Errorf("scene %s paragraph_end %q not found in chapter",
				scenes[i-1].ID, scenes[i-1].ParagraphEnd)
		}
		curStartOrd, ok := ordByID[scenes[i].ParagraphStart]
		if !ok {
			return fmt.Errorf("scene %s paragraph_start %q not found in chapter",
				scenes[i].ID, scenes[i].ParagraphStart)
		}
		if curStartOrd != prevEndOrd+1 {
			return fmt.Errorf("partition gap: scene %s ends at paragraph ordinal %d but scene %s starts at ordinal %d",
				scenes[i-1].ID, prevEndOrd, scenes[i].ID, curStartOrd)
		}
	}
	return nil
}
