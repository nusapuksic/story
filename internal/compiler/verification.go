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

// SceneCardVerification records the result of a verification pass over a scene card.
type SceneCardVerification struct {
	RunID                  string `json:"run_id"`
	Model                  string `json:"model"`
	PromptVersion          string `json:"prompt_version"`
	VerifiedAt             string `json:"verified_at"`
	Supported              bool   `json:"supported"`
	SupportLevel           string `json:"support_level"`
	EpistemicType          string `json:"epistemic_type"`
	Overstatement          string `json:"overstatement,omitempty"`
	MissingCounterevidence bool   `json:"missing_counterevidence"`
}

type rawVerification struct {
	Supported              bool            `json:"supported"`
	SupportLevel           flexibleString  `json:"support_level"`
	EpistemicType          flexibleString  `json:"epistemic_type"`
	Overstatement          *flexibleString `json:"overstatement"`
	MissingCounterevidence bool            `json:"missing_counterevidence"`
}

func compileVerification(
	ctx context.Context,
	p *project.Project,
	st *store.Store,
	chapters []store.ChapterRow,
	opts Options,
	cfg sceneDetectConfig,
	run *Run,
) (int, error) {
	scenesFile, err := openAppendJSONL(p.Path(filepath.Join(project.ModelDir, "scenes.jsonl")))
	if err != nil {
		return 0, err
	}
	defer scenesFile.Close()

	mode := verificationModeForLayer(opts)
	total := 0
	for chapterIndex, ch := range chapters {
		scenes, err := st.ScenesByChapter(ch.ID)
		if err != nil {
			return total, err
		}
		if len(scenes) == 0 {
			reportProgress(opts, ProgressEvent{Layer: LayerVerification, Stage: "item-skip", ChapterID: ch.ID, Current: chapterIndex + 1, Total: len(chapters), Message: fmt.Sprintf("Verification %s (%d/%d): no scenes", ch.ID, chapterIndex+1, len(chapters))})
			continue
		}
		reportProgress(opts, ProgressEvent{Layer: LayerVerification, Stage: "chapter-start", ChapterID: ch.ID, Current: chapterIndex + 1, Total: len(chapters), Message: fmt.Sprintf("Verification %s (%d/%d): checking %d scene(s)", ch.ID, chapterIndex+1, len(chapters), len(scenes))})

		cardsSeen := 0
		for sceneIndex, sc := range scenes {
			card, err := st.InspectSceneCard(sc.ID)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return total, err
			}
			cardsSeen++
			if !opts.Force && isVerifiedSceneCardStatus(card.Status) {
				reportProgress(opts, ProgressEvent{Layer: LayerVerification, Stage: "item-skip", ChapterID: ch.ID, SceneID: sc.ID, Current: sceneIndex + 1, Total: len(scenes), Message: fmt.Sprintf("Verification %s %d/%d: already verified", sc.ID, sceneIndex+1, len(scenes))})
				continue
			}

			cardRecord := sceneCardRecordFromRow(card)
			if !shouldVerifySceneCardForMode(cardRecord, mode) {
				reportProgress(opts, ProgressEvent{Layer: LayerVerification, Stage: "item-skip", ChapterID: ch.ID, SceneID: sc.ID, Current: sceneIndex + 1, Total: len(scenes), Message: fmt.Sprintf("Verification %s %d/%d: not selected by verification_mode=%s", sc.ID, sceneIndex+1, len(scenes), mode)})
				continue
			}

			evidenceParagraphs, err := verificationEvidenceParagraphs(st, sc, card.Evidence)
			if err != nil {
				return total, err
			}
			reportProgress(opts, ProgressEvent{Layer: LayerVerification, Stage: "item-start", ChapterID: ch.ID, SceneID: sc.ID, Current: sceneIndex + 1, Total: len(scenes), Message: fmt.Sprintf("Verification %s %d/%d: verifying scene card", sc.ID, sceneIndex+1, len(scenes))})
			verification, err := verifySceneCard(ctx, p, cardRecord, evidenceParagraphs,
				opts.VerificationProvider, opts.VerificationModel, cfg, run)
			if err != nil {
				return total, fmt.Errorf("verify scene card %s: %w", card.SceneID, err)
			}

			updated := cardRecord
			updated.Verification = &verification
			updated.Status = verificationStatus(verification.Supported, p.Config.Compile.AutoAcceptVerified)
			rawBytes, _ := json.Marshal(updated)
			row := store.SceneCardRow{
				SceneID:         updated.SceneID,
				Title:           updated.Title,
				Summary:         updated.Summary,
				Evidence:        updated.Evidence,
				GenerationRun:   updated.Generation.RunID,
				GenerationModel: updated.Generation.Model,
				PromptVersion:   updated.Generation.PromptVersion,
				Status:          updated.Status,
				RawJSON:         string(rawBytes),
			}
			if err := st.InsertSceneCard(row); err != nil {
				return total, err
			}
			if err := appendJSONL(scenesFile, updated); err != nil {
				return total, err
			}
			total++
			reportProgress(opts, ProgressEvent{Layer: LayerVerification, Stage: "item-complete", ChapterID: ch.ID, SceneID: sc.ID, Current: sceneIndex + 1, Total: len(scenes), Message: fmt.Sprintf("Verification %s %d/%d: completed (%s)", sc.ID, sceneIndex+1, len(scenes), updated.Status)})
		}
		if opts.Layer == LayerVerification && cardsSeen == 0 {
			return total, fmt.Errorf("no scene cards found for chapter %s; run 'story compile --layer scene-cards' first", ch.ID)
		}
	}
	return total, nil
}

func verifySceneCard(
	ctx context.Context,
	p *project.Project,
	card SceneCardRecord,
	evidenceParagraphs []store.ParagraphRow,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
) (SceneCardVerification, error) {
	if prov == nil {
		return SceneCardVerification{}, fmt.Errorf("no LLM provider: cannot verify scene card %s", card.SceneID)
	}
	loadedPrompt := loadCompilerPrompt(p, storyprompts.RecordVerification)
	taskID := ids.NewTaskID()
	req := provider.GenerationRequest{
		Model: model,
		Messages: []provider.Message{
			{Role: "system", Content: loadedPrompt.Content},
			{Role: "user", Content: buildSceneCardVerificationPrompt(card, evidenceParagraphs)},
		},
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxOutputTokens,
		JSONMode:    true,
	}
	resp, timing, err := generateWithAudit(ctx, run, taskID, prov, req)
	if err != nil {
		recordVerificationTask(run, taskID, card.SceneID, TaskStatusFailed, loadedPrompt.Version, err.Error(), timing)
		return SceneCardVerification{}, fmt.Errorf("verification LLM call for scene card %s: %w", card.SceneID, err)
	}
	verification, parseErr := parseVerificationResponse(resp.Content, runID(run), model, loadedPrompt.Version)
	status := TaskStatusCompleted
	errMsg := ""
	if parseErr != nil {
		status = TaskStatusFailed
		errMsg = parseErr.Error()
	}
	recordVerificationTask(run, taskID, card.SceneID, status, loadedPrompt.Version, errMsg, timing)
	return verification, parseErr
}

func parseVerificationResponse(content, runID, model, promptVersion string) (SceneCardVerification, error) {
	raw, err := decodeVerificationContent(stripJSONFences(content))
	if err != nil {
		return SceneCardVerification{}, err
	}
	overstatement := ""
	if raw.Overstatement != nil {
		overstatement = strings.TrimSpace(string(*raw.Overstatement))
	}
	return SceneCardVerification{
		RunID:                  runID,
		Model:                  model,
		PromptVersion:          promptVersion,
		VerifiedAt:             time.Now().UTC().Format(time.RFC3339),
		Supported:              raw.Supported,
		SupportLevel:           normalizeSupportLevel(string(raw.SupportLevel), raw.Supported),
		EpistemicType:          normalizeEpistemicType(string(raw.EpistemicType)),
		Overstatement:          overstatement,
		MissingCounterevidence: raw.MissingCounterevidence,
	}, nil
}

func decodeVerificationContent(content string) (rawVerification, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &obj); err != nil {
		return rawVerification{}, fmt.Errorf("parse verification response: %w", err)
	}
	for _, key := range []string{"verification", "record_verification", "result"} {
		data, ok := obj[key]
		if !ok {
			continue
		}
		var raw rawVerification
		if err := json.Unmarshal(data, &raw); err != nil {
			return rawVerification{}, fmt.Errorf("parse verification response %q: %w", key, err)
		}
		return raw, nil
	}
	if _, ok := obj["supported"]; !ok {
		return rawVerification{}, fmt.Errorf("parse verification response: missing supported")
	}
	var raw rawVerification
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return rawVerification{}, fmt.Errorf("parse verification response: %w", err)
	}
	return raw, nil
}

func buildSceneCardVerificationPrompt(card SceneCardRecord, evidenceParagraphs []store.ParagraphRow) string {
	type proposedSceneCard struct {
		RecordType   string   `json:"record_type"`
		SceneID      string   `json:"scene_id"`
		Title        string   `json:"title"`
		Summary      string   `json:"summary"`
		POV          []string `json:"pov,omitempty"`
		Participants []string `json:"participants,omitempty"`
		Locations    []string `json:"locations,omitempty"`
		Unresolved   []string `json:"unresolved,omitempty"`
		Evidence     []string `json:"evidence"`
	}
	payload := proposedSceneCard{
		RecordType:   "scene_card",
		SceneID:      card.SceneID,
		Title:        card.Title,
		Summary:      card.Summary,
		POV:          card.POV,
		Participants: card.Participants,
		Locations:    card.Locations,
		Unresolved:   card.Unresolved,
		Evidence:     card.Evidence,
	}
	data, _ := json.MarshalIndent(payload, "", "  ")

	var sb strings.Builder
	sb.WriteString("Verify whether the cited paragraphs support this generated scene_card record.\n")
	sb.WriteString("Return JSON matching the schema:\n")
	sb.WriteString(`{"supported":true,"support_level":"explicit|inference|unsupported","epistemic_type":"scene_card","overstatement":null,"missing_counterevidence":false}`)
	sb.WriteString("\nOnly use the cited paragraph excerpts below. If citations are empty or insufficient, mark supported=false.\n\n")
	sb.WriteString("Proposed record JSON:\n")
	sb.Write(data)
	sb.WriteString("\n\nCited paragraph excerpts:\n")
	writeParagraphExcerpts(&sb, evidenceParagraphs)
	return sb.String()
}

func sceneCardRecordFromRow(row store.SceneCardRow) SceneCardRecord {
	var rec SceneCardRecord
	if strings.TrimSpace(row.RawJSON) != "" {
		_ = json.Unmarshal([]byte(row.RawJSON), &rec)
	}
	rec.RecordType = "scene_card"
	rec.SceneID = row.SceneID
	rec.Title = row.Title
	rec.Summary = row.Summary
	rec.Evidence = row.Evidence
	if rec.Generation.RunID == "" {
		rec.Generation.RunID = row.GenerationRun
	}
	if rec.Generation.Model == "" {
		rec.Generation.Model = row.GenerationModel
	}
	if rec.Generation.PromptVersion == "" {
		rec.Generation.PromptVersion = row.PromptVersion
	}
	rec.Status = row.Status
	return rec
}

func verificationEvidenceParagraphs(st *store.Store, scene store.SceneRow, evidence []string) ([]store.ParagraphRow, error) {
	start, err := st.InspectParagraph(scene.ParagraphStart)
	if err != nil {
		return nil, err
	}
	end, err := st.InspectParagraph(scene.ParagraphEnd)
	if err != nil {
		return nil, err
	}
	out := make([]store.ParagraphRow, 0, len(evidence))
	for _, pid := range evidence {
		pp, err := st.InspectParagraph(pid)
		if err != nil {
			return nil, err
		}
		if pp.ChapterID != scene.ChapterID || pp.Ordinal < start.Ordinal || pp.Ordinal > end.Ordinal {
			return nil, fmt.Errorf("scene card %s evidence paragraph %q is outside scene range", scene.ID, pid)
		}
		out = append(out, pp)
	}
	return out, nil
}

func verificationStatus(supported, autoAccept bool) string {
	if !supported {
		return "rejected"
	}
	if autoAccept {
		return "accepted"
	}
	return "verified"
}

func isVerifiedSceneCardStatus(status string) bool {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "verified", "accepted", "rejected":
		return true
	default:
		return false
	}
}

func verificationModeForLayer(opts Options) string {
	if opts.Layer == LayerVerification {
		return VerificationModeAll
	}
	if strings.TrimSpace(opts.VerificationMode) == "" {
		return VerificationModeAll
	}
	return opts.VerificationMode
}

func shouldVerifySceneCardForMode(card SceneCardRecord, mode string) bool {
	switch mode {
	case VerificationModeOff:
		return false
	case VerificationModeRecovered:
		return isRecoveredSceneCard(card)
	case VerificationModeSelective:
		return isRecoveredSceneCard(card) || isSuspiciousSceneCard(card)
	case VerificationModeAll, "":
		return true
	default:
		return true
	}
}

func isRecoveredSceneCard(card SceneCardRecord) bool {
	if card.Recovery == nil {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(card.Recovery.Action)) {
	case "retry", "compact-retry", "fallback":
		return true
	default:
		return false
	}
}

func isSuspiciousSceneCard(card SceneCardRecord) bool {
	if len(card.Evidence) == 0 {
		return true
	}
	if len([]rune(strings.TrimSpace(card.Summary))) < 40 {
		return true
	}
	if len(card.Participants)+len(card.Unresolved) > 16 {
		return true
	}
	return false
}

func normalizeSupportLevel(value string, supported bool) string {
	v := strings.TrimSpace(strings.ToLower(value))
	v = strings.ReplaceAll(v, " ", "-")
	v = strings.ReplaceAll(v, "_", "-")
	switch v {
	case "explicit", "inference", "unsupported":
		return v
	case "explicit-fact", "direct", "direct-support":
		return "explicit"
	case "reasonable-inference", "inferential", "indirect":
		return "inference"
	}
	if !supported {
		return "unsupported"
	}
	return "inference"
}

func normalizeEpistemicType(value string) string {
	v := strings.TrimSpace(strings.ToLower(value))
	v = strings.ReplaceAll(v, " ", "-")
	if v == "" {
		return "scene_card"
	}
	return v
}

func recordVerificationTask(run *Run, taskID, sceneID, status, promptVersion, errMsg string, timings ...taskTiming) {
	if run == nil {
		return
	}
	record := TaskRecord{
		TaskID:        taskID,
		RunID:         runID(run),
		TaskType:      "record-verification",
		SceneID:       sceneID,
		RecordID:      sceneID,
		PromptVersion: promptVersion,
		Status:        status,
		Error:         errMsg,
	}
	if len(timings) > 0 {
		timings[0].applyTo(&record)
	}
	_ = run.recordTask(record)
}
