package compiler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/project"
	storyprompts "github.com/nusapuksic/story/internal/prompts"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/store"
)

// Scene-card failure policies.
const (
	// SceneCardFailurePolicyRetryFallback retries invalid scene-card model output once,
	// then writes a deterministic valid fallback card if the retry still fails.
	SceneCardFailurePolicyRetryFallback = "retry-fallback"
	// SceneCardFailurePolicyStrict preserves developer/debug behavior by failing
	// the compile on invalid scene-card model output.
	SceneCardFailurePolicyStrict = "strict"
)

// SceneCardRecord represents one scene card in model/scenes.jsonl.
type SceneCardRecord struct {
	RecordType   string                 `json:"record_type"` // "scene_card"
	SceneID      string                 `json:"scene_id"`
	Title        string                 `json:"title"`
	Summary      string                 `json:"summary"`
	POV          []string               `json:"pov,omitempty"`
	Participants []string               `json:"participants,omitempty"`
	Locations    []string               `json:"locations,omitempty"`
	Unresolved   []string               `json:"unresolved,omitempty"`
	Evidence     []string               `json:"evidence"`
	Generation   SceneCardGeneration    `json:"generation"`
	Verification *SceneCardVerification `json:"verification,omitempty"`
	Recovery     *SceneCardRecovery     `json:"recovery,omitempty"`
	Status       string                 `json:"status"` // "generated"
}

// SceneCardRecovery describes a successful recovery from invalid scene-card model output.
type SceneCardRecovery struct {
	Policy   string `json:"policy"`
	Action   string `json:"action"`
	Attempts int    `json:"attempts"`
	Reason   string `json:"reason,omitempty"`
}

// SceneCardGeneration is the provenance section of a scene card.
type SceneCardGeneration struct {
	RunID         string `json:"run_id"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
}

// rawSceneCard is the LLM-returned JSON before validation.
type rawSceneCard struct {
	Title        flexibleString       `json:"title"`
	Summary      flexibleString       `json:"summary"`
	POV          flexibleStringList   `json:"pov"`
	Participants flexibleStringList   `json:"participants"`
	Locations    flexibleStringList   `json:"locations"`
	Unresolved   flexibleStringList   `json:"unresolved"`
	Evidence     flexibleEvidenceList `json:"evidence"`
}

type flexibleString string

func (s *flexibleString) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = flexibleString(text)
		return nil
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = flexibleString(jsonText(value))
	return nil
}

type flexibleStringList []string

func (s *flexibleStringList) UnmarshalJSON(data []byte) error {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err == nil {
		out := make([]string, 0, len(items))
		for _, item := range items {
			var text flexibleString
			if err := json.Unmarshal(item, &text); err != nil {
				return err
			}
			if value := strings.TrimSpace(string(text)); value != "" {
				out = append(out, value)
			}
		}
		*s = out
		return nil
	}

	var text flexibleString
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	if value := strings.TrimSpace(string(text)); value != "" {
		*s = []string{value}
	} else {
		*s = nil
	}
	return nil
}

type flexibleEvidenceList []string

func (s *flexibleEvidenceList) UnmarshalJSON(data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = dedupeStrings(extractEvidenceIDs(value))
	return nil
}

func extractEvidenceIDs(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		if text := strings.TrimSpace(v); text != "" {
			return []string{text}
		}
	case []any:
		var out []string
		for _, item := range v {
			out = append(out, extractEvidenceIDs(item)...)
		}
		return out
	case map[string]any:
		return extractEvidenceIDsFromObject(v)
	}
	return nil
}

func extractEvidenceIDsFromObject(value map[string]any) []string {
	var out []string
	for _, key := range []string{
		"paragraph_id",
		"paragraphId",
		"paragraphID",
		"paragraph_ids",
		"paragraphIds",
		"paragraphIDs",
		"paragraph",
		"paragraphs",
		"id",
		"ids",
		"source",
		"sources",
		"source_paragraph",
		"sourceParagraph",
		"source_paragraphs",
		"sourceParagraphs",
		"citation",
		"citations",
		"evidence",
	} {
		if nested, ok := value[key]; ok {
			out = append(out, extractEvidenceIDs(nested)...)
		}
	}

	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if looksLikeParagraphID(key) {
			out = append(out, key)
		}
	}
	return out
}

func looksLikeParagraphID(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "p-")
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func jsonText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case bool:
		return fmt.Sprint(v)
	case float64:
		return fmt.Sprint(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := jsonText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "; ")
	case map[string]any:
		return objectText(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func objectText(value map[string]any) string {
	for _, key := range []string{
		"plot_overview",
		"summary",
		"action",
		"description",
		"text",
		"statement",
		"title",
		"name",
		"value",
		"paragraph_id",
		"id",
	} {
		if text := jsonText(value[key]); text != "" {
			return text
		}
	}

	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if text := jsonText(value[key]); text != "" {
			parts = append(parts, key+": "+text)
		}
	}
	return strings.Join(parts, "; ")
}

// extractSceneCard calls the LLM extraction prompt for one scene, validates
// the response, and returns a SceneCardRecord. Invalid model output is retried
// once by default, then replaced with a deterministic valid fallback card.
// Timeout failures get a compact-context retry before falling back.
func extractSceneCard(
	ctx context.Context,
	p *project.Project,
	scene store.SceneRow,
	paragraphs []store.ParagraphRow,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
	policy string,
) (*SceneCardRecord, error) {
	if prov == nil {
		return nil, fmt.Errorf("no LLM provider: cannot extract scene card for %s", scene.ID)
	}
	policy, err := normalizeSceneCardFailurePolicy(policy)
	if err != nil {
		return nil, err
	}

	pidSet := sceneCardParagraphIDSet(paragraphs)
	loadedPrompt := loadCompilerPrompt(p, storyprompts.SceneExtraction)
	card, parseErr, genErr := generateSceneCardAttempt(ctx, scene.ID, buildSceneCardPrompt(scene, paragraphs),
		prov, model, cfg, loadedPrompt, pidSet, paragraphs, run, "scene-extraction")
	if genErr != nil {
		if policy == SceneCardFailurePolicyStrict || !isTimeoutError(genErr) {
			return nil, genErr
		}
		return retrySceneCardAfterTimeout(ctx, scene, paragraphs, prov, model, cfg, loadedPrompt, run, policy, genErr)
	}
	if parseErr == nil {
		return card, nil
	}
	if policy == SceneCardFailurePolicyStrict {
		return nil, parseErr
	}

	retryPrompt := buildSceneCardRetryPrompt(scene, paragraphs, parseErr)
	retried, retryParseErr, retryGenErr := generateSceneCardAttempt(ctx, scene.ID, retryPrompt,
		prov, model, cfg, loadedPrompt, pidSet, paragraphs, run, "scene-extraction-retry")
	if retryGenErr == nil && retryParseErr == nil {
		retried.Recovery = &SceneCardRecovery{
			Policy:   policy,
			Action:   "retry",
			Attempts: 2,
			Reason:   parseErr.Error(),
		}
		return retried, nil
	}
	if retryGenErr != nil && !isTimeoutError(retryGenErr) {
		return nil, retryGenErr
	}

	reason := sceneCardRecoveryReason(parseErr, retryParseErr, retryGenErr)
	fallback := fallbackSceneCardFromSceneText(scene.ID, paragraphs, runID(run), model, loadedPrompt.Version)
	fallback.Recovery = &SceneCardRecovery{
		Policy:   policy,
		Action:   "fallback",
		Attempts: 2,
		Reason:   reason,
	}
	recordSceneCardFallbackTask(run, scene.ID, loadedPrompt.Version, reason)
	return fallback, nil
}

func retrySceneCardAfterTimeout(
	ctx context.Context,
	scene store.SceneRow,
	paragraphs []store.ParagraphRow,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	loadedPrompt storyprompts.Loaded,
	run *Run,
	policy string,
	initialErr error,
) (*SceneCardRecord, error) {
	compactParagraphs := compactSceneCardParagraphs(paragraphs)
	retried, retryParseErr, retryGenErr := generateSceneCardAttempt(ctx, scene.ID,
		buildSceneCardTimeoutRetryPrompt(scene, compactParagraphs, initialErr),
		prov, model, cfg, loadedPrompt, sceneCardParagraphIDSet(compactParagraphs), compactParagraphs, run, "scene-extraction-timeout-retry")
	if retryGenErr == nil && retryParseErr == nil {
		retried.Recovery = &SceneCardRecovery{
			Policy:   policy,
			Action:   "compact-retry",
			Attempts: 2,
			Reason:   "initial call: " + initialErr.Error(),
		}
		return retried, nil
	}
	if retryGenErr != nil && !isTimeoutError(retryGenErr) {
		return nil, retryGenErr
	}

	reason := sceneCardCallRecoveryReason(initialErr, retryParseErr, retryGenErr)
	fallback := fallbackSceneCardFromSceneText(scene.ID, paragraphs, runID(run), model, loadedPrompt.Version)
	fallback.Recovery = &SceneCardRecovery{
		Policy:   policy,
		Action:   "fallback",
		Attempts: 2,
		Reason:   reason,
	}
	recordSceneCardFallbackTask(run, scene.ID, loadedPrompt.Version, reason)
	return fallback, nil
}

func sceneCardParagraphIDSet(paragraphs []store.ParagraphRow) map[string]bool {
	pidSet := make(map[string]bool, len(paragraphs))
	for _, p := range paragraphs {
		pidSet[p.ID] = true
	}
	return pidSet
}

func generateSceneCardAttempt(
	ctx context.Context,
	sceneID string,
	prompt string,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	loadedPrompt storyprompts.Loaded,
	pidSet map[string]bool,
	paragraphs []store.ParagraphRow,
	run *Run,
	taskType string,
) (*SceneCardRecord, error, error) {
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
		_ = run.saveRawResponse(taskID, resp)
	}
	if err != nil {
		recordSceneCardTask(run, taskID, sceneID, taskType, TaskStatusFailed, loadedPrompt.Version, err.Error())
		return nil, nil, fmt.Errorf("scene card LLM call for scene %s: %w", sceneID, err)
	}

	card, parseErr := parseSceneCardResponse(resp.Content, sceneID, pidSet, paragraphs, runID(run), model, loadedPrompt.Version)
	status := TaskStatusCompleted
	errMsg := ""
	if parseErr != nil {
		status = TaskStatusFailed
		errMsg = parseErr.Error()
	}
	recordSceneCardTask(run, taskID, sceneID, taskType, status, loadedPrompt.Version, errMsg)
	return card, parseErr, nil
}

func recordSceneCardTask(run *Run, taskID, sceneID, taskType, status, promptVersion, errMsg string) {
	if run == nil {
		return
	}
	_ = run.recordTask(TaskRecord{
		TaskID:        taskID,
		RunID:         runID(run),
		TaskType:      taskType,
		SceneID:       sceneID,
		Status:        status,
		PromptVersion: promptVersion,
		Error:         errMsg,
	})
}

func recordSceneCardFallbackTask(run *Run, sceneID, promptVersion, reason string) {
	recordSceneCardTask(run, ids.NewTaskID(), sceneID, "scene-extraction-fallback", TaskStatusCompleted, promptVersion, reason)
}

func sceneCardRecoveryReason(firstErr, retryParseErr, retryGenErr error) string {
	parts := []string{"initial validation: " + firstErr.Error()}
	if retryGenErr != nil {
		parts = append(parts, "retry call: "+retryGenErr.Error())
	} else if retryParseErr != nil {
		parts = append(parts, "retry validation: "+retryParseErr.Error())
	}
	return strings.Join(parts, "; ")
}

func sceneCardCallRecoveryReason(firstErr, retryParseErr, retryGenErr error) string {
	parts := []string{"initial call: " + firstErr.Error()}
	if retryGenErr != nil {
		parts = append(parts, "compact retry call: "+retryGenErr.Error())
	} else if retryParseErr != nil {
		parts = append(parts, "compact retry validation: "+retryParseErr.Error())
	}
	return strings.Join(parts, "; ")
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "client.timeout exceeded") ||
		strings.Contains(msg, "timeout exceeded") ||
		strings.Contains(msg, "timed out")
}

func sceneCardFailurePolicy(p *project.Project, opts Options) (string, error) {
	policy := opts.SceneCardFailurePolicy
	if strings.TrimSpace(policy) == "" && p != nil {
		policy = p.Config.Compile.SceneCardFailurePolicy
	}
	return normalizeSceneCardFailurePolicy(policy)
}

func normalizeSceneCardFailurePolicy(policy string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(policy))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "", SceneCardFailurePolicyRetryFallback, "retry-and-fallback", "retry-then-fallback":
		return SceneCardFailurePolicyRetryFallback, nil
	case SceneCardFailurePolicyStrict, "strict-fail":
		return SceneCardFailurePolicyStrict, nil
	default:
		return "", fmt.Errorf("unknown scene card failure policy %q; supported: %s, %s",
			policy, SceneCardFailurePolicyRetryFallback, SceneCardFailurePolicyStrict)
	}
}

// parseSceneCardResponse parses and validates the LLM response for scene card
// extraction.  It verifies that every evidence paragraph ID exists in pidSet.
func parseSceneCardResponse(
	content, sceneID string,
	pidSet map[string]bool,
	paragraphs []store.ParagraphRow,
	runID, model, promptVersion string,
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
	title := strings.TrimSpace(string(raw.Title))
	summary := strings.TrimSpace(string(raw.Summary))
	if summary == "" {
		summary = deriveSceneCardSummary(title, paragraphs, sceneID)
	}
	if title == "" {
		title = deriveSceneCardTitle(summary, sceneID)
	}
	// Validate evidence paragraph IDs.
	evidence := []string(raw.Evidence)
	for _, pid := range evidence {
		if !pidSet[pid] {
			return nil, fmt.Errorf("scene card for %s: evidence cites unknown paragraph ID %q", sceneID, pid)
		}
	}

	return &SceneCardRecord{
		RecordType:   "scene_card",
		SceneID:      sceneID,
		Title:        title,
		Summary:      summary,
		POV:          []string(raw.POV),
		Participants: []string(raw.Participants),
		Locations:    []string(raw.Locations),
		Unresolved:   []string(raw.Unresolved),
		Evidence:     evidence,
		Generation: SceneCardGeneration{
			RunID:         runID,
			Model:         model,
			PromptVersion: promptVersion,
		},
		Status: "generated",
	}, nil
}

func isTruncatedJSONError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "unexpected end of JSON input") || strings.Contains(msg, "unexpected EOF")
}

func fallbackSceneCardFromSceneText(sceneID string, paragraphs []store.ParagraphRow, runID, model, promptVersion string) *SceneCardRecord {
	summary, evidence := deriveSceneTextSummaryEvidence(paragraphs)
	if summary == "" {
		summary = fallbackSceneCardTitle(sceneID) + "."
	}
	return &SceneCardRecord{
		RecordType: "scene_card",
		SceneID:    sceneID,
		Title:      deriveSceneCardTitle(summary, sceneID),
		Summary:    summary,
		Evidence:   evidence,
		Generation: SceneCardGeneration{
			RunID:         runID,
			Model:         model,
			PromptVersion: promptVersion,
		},
		Status: "generated",
	}
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
	summary, _ := deriveSceneTextSummaryEvidence(paragraphs)
	return summary
}

func deriveSceneTextSummaryEvidence(paragraphs []store.ParagraphRow) (string, []string) {
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
		if p.ID == "" {
			return text, nil
		}
		return text, []string{p.ID}
	}
	return "", nil
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

func buildSceneCardRetryPrompt(scene store.SceneRow, paragraphs []store.ParagraphRow, _ error) string {
	var sb strings.Builder
	sb.WriteString("The previous scene-card response cited paragraph IDs outside the allowed list and was rejected.\n")
	sb.WriteString("Return a corrected scene card. Cite only paragraph IDs from the allowed list below.\n\n")
	sb.WriteString(buildSceneCardPrompt(scene, paragraphs))
	return sb.String()
}

func buildSceneCardTimeoutRetryPrompt(scene store.SceneRow, paragraphs []store.ParagraphRow, _ error) string {
	var sb strings.Builder
	sb.WriteString("The previous scene-card request timed out before a response was returned.\n")
	sb.WriteString("Retry with this compact evidence packet. The paragraph text has been selected and trimmed to reduce context. Return conservative JSON and cite only listed paragraph IDs.\n\n")
	sb.WriteString(buildSceneCardPrompt(scene, paragraphs))
	return sb.String()
}

func compactSceneCardParagraphs(paragraphs []store.ParagraphRow) []store.ParagraphRow {
	const (
		maxParagraphs     = 12
		headParagraphs    = 8
		tailParagraphs    = 4
		maxParagraphRunes = 600
	)
	if len(paragraphs) == 0 {
		return nil
	}

	selected := make(map[int]bool, maxParagraphs)
	if len(paragraphs) <= maxParagraphs {
		for i := range paragraphs {
			selected[i] = true
		}
	} else {
		for i := 0; i < headParagraphs && i < len(paragraphs); i++ {
			selected[i] = true
		}
		for i := len(paragraphs) - tailParagraphs; i < len(paragraphs); i++ {
			if i >= 0 {
				selected[i] = true
			}
		}
	}

	out := make([]store.ParagraphRow, 0, len(selected))
	for i, p := range paragraphs {
		if !selected[i] {
			continue
		}
		pp := p
		pp.Text = compactSceneCardText(p.Text, maxParagraphRunes)
		out = append(out, pp)
	}
	return out
}

func compactSceneCardText(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(text), " ")
	if maxRunes <= 0 {
		return text
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
	sb.WriteString("\n\nCite paragraph IDs for every concrete statement. Use only IDs from the allowed list below.")
	sb.WriteString("\n\nAllowed evidence paragraph IDs:\n")
	for _, p := range paragraphs {
		if strings.TrimSpace(p.ID) == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(p.ID)
		sb.WriteString("\n")
	}
	sb.WriteString("\nParagraph excerpts:\n")
	for _, p := range paragraphs {
		sb.WriteString("--- ")
		sb.WriteString(p.ID)
		sb.WriteString(" ---\n")
		sb.WriteString(sanitizeSceneCardPromptText(p.Text))
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func sanitizeSceneCardPromptText(text string) string {
	return paragraphIDPattern.ReplaceAllString(text, "[paragraph-id-redacted]")
}
