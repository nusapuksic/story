package compiler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/project"
	storyprompts "github.com/nusapuksic/story/internal/prompts"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/store"
)

const (
	CharacterClassificationPrincipal       = "principal"
	CharacterClassificationMajorSupporting = "major_supporting"
	CharacterClassificationSupporting      = "supporting"
	CharacterClassificationMinor           = "minor"
	CharacterClassificationUncertain       = "uncertain"

	principalCharacterClassificationMaxAttempts = 3
	principalCharacterTaskType                  = "principal-characters"
	principalCharacterRetryTaskType             = "principal-characters-retry"
)

// CharacterRoleRecord is one book-level character identity and narrative-role assessment.
type CharacterRoleRecord struct {
	RecordType      string                  `json:"record_type"`
	CharacterID     string                  `json:"character_id"`
	SourceEntityIDs []string                `json:"source_entity_ids"`
	CanonicalName   string                  `json:"canonical_name"`
	Aliases         []string                `json:"aliases,omitempty"`
	Classification  string                  `json:"classification"`
	Role            string                  `json:"role,omitempty"`
	Confidence      float64                 `json:"confidence"`
	Rationale       string                  `json:"rationale"`
	Evidence        []CharacterRoleEvidence `json:"evidence,omitempty"`
	Generation      CharacterRoleGeneration `json:"generation"`
	Status          string                  `json:"status"`
}

// CharacterRoleEvidence is scene-scoped support for a role assessment.
type CharacterRoleEvidence struct {
	SceneID string `json:"scene_id"`
	Reason  string `json:"reason"`
}

// CharacterRoleGeneration stores provenance for a generated role assessment.
type CharacterRoleGeneration struct {
	RunID         string `json:"run_id"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
	GeneratedAt   string `json:"generated_at"`
}

// CharacterRolesSnapshotRecord marks the latest complete character-role assessment batch.
type CharacterRolesSnapshotRecord struct {
	RecordType    string `json:"record_type"`
	RunID         string `json:"run_id"`
	RoleCount     int    `json:"role_count"`
	InputHash     string `json:"input_hash"`
	ArtifactHash  string `json:"artifact_hash"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
	CommittedAt   string `json:"committed_at"`
}

type rawPrincipalResponse struct {
	Characters     []rawCharacterRole `json:"characters"`
	CharacterRoles []rawCharacterRole `json:"character_roles"`
	Principals     []rawCharacterRole `json:"principals"`
	Roles          []rawCharacterRole `json:"roles"`
}

type rawCharacterRole struct {
	SourceEntityIDs flexibleStringList         `json:"source_entity_ids"`
	SourceEntityID  flexibleString             `json:"source_entity_id"`
	EntityIDs       flexibleStringList         `json:"entity_ids"`
	EntityID        flexibleString             `json:"entity_id"`
	CanonicalName   flexibleString             `json:"canonical_name"`
	Name            flexibleString             `json:"name"`
	Classification  flexibleString             `json:"classification"`
	Role            flexibleString             `json:"role"`
	Confidence      float64                    `json:"confidence"`
	Rationale       flexibleString             `json:"rationale"`
	Evidence        []rawCharacterRoleEvidence `json:"evidence"`
}

type rawCharacterRoleEvidence struct {
	SceneID flexibleString `json:"scene_id"`
	Reason  flexibleString `json:"reason"`
}

type characterRoleInputSet struct {
	SourceEntities []principalSourceEntity `json:"source_entities"`
	SceneContext   []principalSceneContext `json:"scene_context"`
	InputHash      string                  `json:"-"`

	EntityByID       map[string]principalSourceEntity `json:"-"`
	SceneIDs         map[string]bool                  `json:"-"`
	SourceScenesByID map[string]map[string]bool       `json:"-"`
}

type principalSourceEntity struct {
	EntityID       string   `json:"entity_id"`
	ChapterID      string   `json:"chapter_id"`
	CanonicalName  string   `json:"canonical_name"`
	Aliases        []string `json:"aliases,omitempty"`
	EvidenceScenes []string `json:"evidence_scenes"`
}

type principalSceneContext struct {
	SceneID      string   `json:"scene_id"`
	ChapterID    string   `json:"chapter_id"`
	Title        string   `json:"title,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	POV          []string `json:"pov,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Entities     []string `json:"entities,omitempty"`
	Unresolved   []string `json:"unresolved,omitempty"`
}

type stagedPrincipalsPayload struct {
	Records  []CharacterRoleRecord        `json:"records"`
	Snapshot CharacterRolesSnapshotRecord `json:"snapshot"`
}

func compilePrincipals(
	ctx context.Context,
	p *project.Project,
	st *store.Store,
	chapters []store.ChapterRow,
	opts Options,
	cfg sceneDetectConfig,
	run *Run,
) (int, error) {
	if strings.TrimSpace(opts.ChapterID) != "" {
		return 0, fmt.Errorf("principals is a book-level compile layer; --chapter is not supported")
	}
	if err := ensureEntitySnapshotsCommitted(st, chapters); err != nil {
		return 0, err
	}

	input, err := buildCharacterRoleInput(st)
	if err != nil {
		return 0, err
	}
	rolesPath := p.Path(filepath.Join(project.ModelDir, "character_roles.jsonl"))
	currentRecords, currentSnapshot, err := ReadLatestCharacterRoles(rolesPath)
	if err != nil {
		return 0, err
	}

	loadedPrompt := loadCompilerPrompt(p, storyprompts.PrincipalCharacters)
	if !opts.Force && characterRolesSnapshotIsCurrent(currentSnapshot, input.InputHash, opts.ExtractionModel, loadedPrompt.Version) {
		reportProgress(opts, ProgressEvent{Layer: LayerPrincipals, Stage: "item-skip", Message: "Principals: already current"})
		return 0, nil
	}

	var records []CharacterRoleRecord
	generation := CharacterRoleGeneration{
		RunID:         runID(run),
		Model:         opts.ExtractionModel,
		PromptVersion: loadedPrompt.Version,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if len(input.SourceEntities) == 0 {
		records = []CharacterRoleRecord{}
	} else {
		if opts.ExtractionProvider == nil {
			return 0, errors.New("no LLM provider configured: principals require an extraction provider; configure [llm] in story.toml")
		}
		reportProgress(opts, ProgressEvent{Layer: LayerPrincipals, Stage: "item-start", Message: fmt.Sprintf("Principals: classifying %d character entity record(s)", len(input.SourceEntities))})
		records, err = extractCharacterRoles(ctx, p, input, currentRecords, opts.ExtractionProvider, opts.ExtractionModel, cfg, loadedPrompt, generation, run, opts.Progress)
		if err != nil {
			return 0, err
		}
	}

	artifactHash, err := hashCharacterRoleRecords(records)
	if err != nil {
		return 0, err
	}
	snapshot := CharacterRolesSnapshotRecord{
		RecordType:    "character_roles_snapshot",
		RunID:         runID(run),
		RoleCount:     len(records),
		InputHash:     input.InputHash,
		ArtifactHash:  artifactHash,
		Model:         opts.ExtractionModel,
		PromptVersion: loadedPrompt.Version,
		CommittedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	staging, err := optionalRunStagingStore(run)
	if err != nil {
		return 0, err
	}
	var staged StagedResultRef
	if staging != nil {
		staged, err = staging.StageJSON(LayerPrincipals, StagedResultMeta{
			Sequence:      0,
			TaskID:        "principal-characters",
			TargetID:      "book",
			TargetHash:    input.InputHash,
			SchemaVersion: 1,
		}, stagedPrincipalsPayload{Records: records, Snapshot: snapshot})
		if err != nil {
			return 0, err
		}
	}

	if err := commitCharacterRoles(rolesPath, records, snapshot); err != nil {
		return 0, err
	}
	if staging != nil {
		if err := staging.RecordCommit(staged); err != nil {
			return 0, err
		}
	}
	reportProgress(opts, ProgressEvent{Layer: LayerPrincipals, Stage: "item-complete", Message: fmt.Sprintf("Principals: committed %d role assessment(s)", len(records))})
	return len(records), nil
}

func ensureEntitySnapshotsCommitted(st *store.Store, chapters []store.ChapterRow) error {
	for _, ch := range chapters {
		committed, err := st.IsEntitySnapshotCommitted(ch.ID)
		if err != nil {
			return err
		}
		if !committed {
			return fmt.Errorf("principals require committed canonical entities for %s; run 'story compile --layer entities' first", ch.ID)
		}
	}
	return nil
}

func buildCharacterRoleInput(st *store.Store) (characterRoleInputSet, error) {
	rows, err := st.EntityRowsForChapter("")
	if err != nil {
		return characterRoleInputSet{}, err
	}
	scenes, err := st.AllScenes()
	if err != nil {
		return characterRoleInputSet{}, err
	}
	sceneChapter := make(map[string]string, len(scenes))
	for _, scene := range scenes {
		sceneChapter[scene.ID] = scene.ChapterID
	}
	cards, err := st.AllSceneCards()
	if err != nil {
		return characterRoleInputSet{}, err
	}
	cardByScene := make(map[string]store.SceneCardRow, len(cards))
	for _, card := range cards {
		cardByScene[card.SceneID] = card
	}

	input := characterRoleInputSet{
		EntityByID:       make(map[string]principalSourceEntity),
		SceneIDs:         make(map[string]bool),
		SourceScenesByID: make(map[string]map[string]bool),
	}
	neededScenes := make(map[string]bool)
	for _, row := range rows {
		if !strings.EqualFold(strings.TrimSpace(row.Type), "character") {
			continue
		}
		entity := principalSourceEntity{
			EntityID:       row.ID,
			ChapterID:      row.ChapterID,
			CanonicalName:  row.CanonicalName,
			Aliases:        dedupeStrings(row.Aliases),
			EvidenceScenes: sortedCleanStrings(row.Evidence),
		}
		sceneSet := make(map[string]bool)
		for _, sceneID := range entity.EvidenceScenes {
			if sceneID == "" {
				continue
			}
			sceneSet[sceneID] = true
			neededScenes[sceneID] = true
		}
		input.SourceEntities = append(input.SourceEntities, entity)
		input.EntityByID[entity.EntityID] = entity
		input.SourceScenesByID[entity.EntityID] = sceneSet
	}

	sort.SliceStable(input.SourceEntities, func(i, j int) bool {
		if strings.ToLower(input.SourceEntities[i].CanonicalName) != strings.ToLower(input.SourceEntities[j].CanonicalName) {
			return strings.ToLower(input.SourceEntities[i].CanonicalName) < strings.ToLower(input.SourceEntities[j].CanonicalName)
		}
		return input.SourceEntities[i].EntityID < input.SourceEntities[j].EntityID
	})

	sceneIDs := make([]string, 0, len(neededScenes))
	for sceneID := range neededScenes {
		sceneIDs = append(sceneIDs, sceneID)
		input.SceneIDs[sceneID] = true
	}
	sort.Strings(sceneIDs)
	for _, sceneID := range sceneIDs {
		ctx := principalSceneContext{SceneID: sceneID, ChapterID: sceneChapter[sceneID]}
		if card, ok := cardByScene[sceneID]; ok {
			ctx = principalSceneContextFromCard(ctx, card)
		}
		input.SceneContext = append(input.SceneContext, ctx)
	}

	input.InputHash, err = hashCharacterRoleInput(input)
	if err != nil {
		return characterRoleInputSet{}, err
	}
	return input, nil
}

func principalSceneContextFromCard(ctx principalSceneContext, card store.SceneCardRow) principalSceneContext {
	ctx.Title = card.Title
	ctx.Summary = stripParagraphRefs(card.Summary)
	var rec SceneCardRecord
	if err := json.Unmarshal([]byte(card.RawJSON), &rec); err == nil {
		ctx.POV = dedupeStrings(rec.POV)
		ctx.Participants = dedupeStrings(rec.Participants)
		ctx.Entities = dedupeStrings(rec.Entities)
		ctx.Unresolved = dedupeStrings(rec.Unresolved)
	}
	return ctx
}

func hashCharacterRoleInput(input characterRoleInputSet) (string, error) {
	payload := struct {
		SourceEntities []principalSourceEntity `json:"source_entities"`
		SceneContext   []principalSceneContext `json:"scene_context"`
	}{SourceEntities: input.SourceEntities, SceneContext: input.SceneContext}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "sha256:" + sha256Hex(data), nil
}

func extractCharacterRoles(
	ctx context.Context,
	p *project.Project,
	input characterRoleInputSet,
	currentRecords []CharacterRoleRecord,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	loadedPrompt storyprompts.Loaded,
	generation CharacterRoleGeneration,
	run *Run,
	progress ProgressFunc,
) ([]CharacterRoleRecord, error) {
	baseMessages := []provider.Message{
		{Role: "system", Content: loadedPrompt.Content},
		{Role: "user", Content: buildCharacterRolePrompt(input)},
	}
	var lastParseErr error
	for attempt := 1; attempt <= principalCharacterClassificationMaxAttempts; attempt++ {
		taskID := ids.NewTaskID()
		messages := append([]provider.Message(nil), baseMessages...)
		if lastParseErr != nil {
			emitProgress(progress, ProgressEvent{
				Layer:   LayerPrincipals,
				Stage:   "item-retry",
				Current: attempt,
				Total:   principalCharacterClassificationMaxAttempts,
				Message: fmt.Sprintf("Principals: retrying classification after invalid model output (attempt %d/%d): %s", attempt, principalCharacterClassificationMaxAttempts, compactProgressError(lastParseErr)),
			})
			messages = append(messages, provider.Message{
				Role:    "user",
				Content: buildCharacterRoleRetryPrompt(input, lastParseErr),
			})
		}
		req := provider.GenerationRequest{
			Model:       model,
			Messages:    messages,
			Temperature: cfg.Temperature,
			MaxTokens:   cfg.MaxOutputTokens,
			JSONMode:    true,
		}
		resp, timing, err := generateWithAudit(ctx, run, taskID, prov, req)
		taskType := characterRoleTaskTypeForAttempt(attempt)
		if err != nil {
			recordCharacterRoleTask(run, taskID, taskType, TaskStatusFailed, loadedPrompt.Version, err.Error(), timing)
			return nil, fmt.Errorf("principal character classification LLM call: %w", err)
		}

		records, parseErr := parseCharacterRoleResponse(resp.Content, input, currentRecords, generation)
		status := TaskStatusCompleted
		errMsg := ""
		if parseErr != nil {
			status = TaskStatusFailed
			errMsg = parseErr.Error()
		}
		recordCharacterRoleTask(run, taskID, taskType, status, loadedPrompt.Version, errMsg, timing)
		if parseErr == nil {
			return records, nil
		}
		lastParseErr = parseErr
	}
	return nil, fmt.Errorf("principal character classification output: failed after %d attempts: %w", principalCharacterClassificationMaxAttempts, lastParseErr)
}

func buildCharacterRolePrompt(input characterRoleInputSet) string {
	var sb strings.Builder
	sb.WriteString("Classify book-level character roles as JSON.\n")
	sb.WriteString("Use the supplied canonical character entity records as candidates. Only combine source_entity_ids when canonical names, aliases, and linked evidence clearly identify the same book-level character. Then classify each book-level character by narrative function.\n")
	sb.WriteString("Return JSON matching the schema:\n")
	sb.WriteString(`{"characters":[{"source_entity_ids":["entity-..."],"canonical_name":"...","classification":"principal|major_supporting|supporting|minor|uncertain","role":"...","confidence":0.9,"rationale":"...","evidence":[{"scene_id":"sc-...","reason":"..."}]}]}`)
	sb.WriteString("\nRules:\n")
	sb.WriteString("- Use every source_entity_id exactly once.\n")
	sb.WriteString("- Use only source_entity_ids and scene IDs listed below.\n")
	sb.WriteString("- Do not redo entity resolution from scene text; aliases are metadata, not candidates.\n")
	sb.WriteString("- Evidence is scene-scoped; do not invent paragraph IDs.\n")
	sb.WriteString("- Classify narrative importance, not frequency or chapter percentage.\n")
	sb.WriteString("- Do not force a fixed number of principal characters.\n\n")
	sb.WriteString("Character entity records:\n")
	for _, entity := range input.SourceEntities {
		sb.WriteString("- ")
		sb.WriteString(entity.EntityID)
		sb.WriteString(" (")
		sb.WriteString(entity.ChapterID)
		sb.WriteString("): ")
		sb.WriteString(entity.CanonicalName)
		if len(entity.Aliases) > 0 {
			sb.WriteString("; aliases: ")
			sb.WriteString(strings.Join(entity.Aliases, "; "))
		}
		if len(entity.EvidenceScenes) > 0 {
			sb.WriteString("; linked scenes: ")
			sb.WriteString(strings.Join(entity.EvidenceScenes, ", "))
		}

		sb.WriteString("\n")
	}
	sb.WriteString("\nLinked scene evidence:\n")
	for _, scene := range input.SceneContext {
		sb.WriteString("- ")
		sb.WriteString(scene.SceneID)
		if scene.ChapterID != "" {
			sb.WriteString(" (")
			sb.WriteString(scene.ChapterID)
			sb.WriteString(")")
		}
		if scene.Title != "" {
			sb.WriteString(" title: ")
			sb.WriteString(scene.Title)
		}
		if scene.Summary != "" {
			sb.WriteString(" summary: ")
			sb.WriteString(scene.Summary)
		}
		if len(scene.POV) > 0 {
			sb.WriteString(" pov: ")
			sb.WriteString(strings.Join(scene.POV, "; "))
		}
		if len(scene.Participants) > 0 {
			sb.WriteString(" participants: ")
			sb.WriteString(strings.Join(scene.Participants, "; "))
		}
		if len(scene.Unresolved) > 0 {
			sb.WriteString(" unresolved: ")
			sb.WriteString(strings.Join(scene.Unresolved, "; "))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func buildCharacterRoleRetryPrompt(input characterRoleInputSet, parseErr error) string {
	var sb strings.Builder
	sb.WriteString("The previous principal character classification JSON response failed validation: ")
	if parseErr != nil {
		sb.WriteString(parseErr.Error())
	} else {
		sb.WriteString("invalid output")
	}
	sb.WriteString("\nRetry the same task. Return a complete replacement JSON object matching the requested schema, not a patch.\n")
	sb.WriteString("Allowed source_entity_ids are: ")
	sb.WriteString(strings.Join(characterRoleSourceEntityIDs(input), ", "))
	sb.WriteString("\nUse every allowed source_entity_id exactly once. Do not use aliases, character names, or invented IDs as source_entity_ids.")
	return sb.String()
}

func characterRoleSourceEntityIDs(input characterRoleInputSet) []string {
	ids := make([]string, 0, len(input.SourceEntities))
	for _, entity := range input.SourceEntities {
		if strings.TrimSpace(entity.EntityID) != "" {
			ids = append(ids, entity.EntityID)
		}
	}
	sort.Strings(ids)
	return ids
}

func compactProgressError(err error) string {
	if err == nil {
		return "invalid output"
	}
	const maxRunes = 240
	text := strings.Join(strings.Fields(err.Error()), " ")
	if text == "" {
		return "invalid output"
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

func parseCharacterRoleResponse(content string, input characterRoleInputSet, currentRecords []CharacterRoleRecord, generation CharacterRoleGeneration) ([]CharacterRoleRecord, error) {
	raw, err := parseRawPrincipalResponse(content)
	if err != nil {
		return nil, err
	}
	roles := raw.Characters
	if len(roles) == 0 {
		roles = raw.CharacterRoles
	}
	if len(roles) == 0 {
		roles = raw.Principals
	}
	if len(roles) == 0 {
		roles = raw.Roles
	}
	if len(roles) == 0 {
		return nil, fmt.Errorf("response contains no character role records")
	}
	return characterRolesFromRaw(roles, input, currentRecords, generation)
}

func parseRawPrincipalResponse(content string) (rawPrincipalResponse, error) {
	content = stripJSONFences(content)
	var raw rawPrincipalResponse
	if err := json.Unmarshal([]byte(content), &raw); err == nil && rawPrincipalRoleCount(raw) > 0 {
		return raw, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return rawPrincipalResponse{}, fmt.Errorf("parse principal response: %w", err)
	}
	for _, key := range []string{"result", "principal_characters", "character_roles"} {
		if data, ok := envelope[key]; ok {
			if err := json.Unmarshal(data, &raw); err == nil {
				return raw, nil
			}
		}
	}
	return raw, nil
}

func rawPrincipalRoleCount(raw rawPrincipalResponse) int {
	return len(raw.Characters) + len(raw.CharacterRoles) + len(raw.Principals) + len(raw.Roles)
}

func characterRolesFromRaw(rawRoles []rawCharacterRole, input characterRoleInputSet, currentRecords []CharacterRoleRecord, generation CharacterRoleGeneration) ([]CharacterRoleRecord, error) {
	existingIDs := existingCharacterIDsBySourceSet(currentRecords)
	seenSourceIDs := make(map[string]bool, len(input.SourceEntities))
	records := make([]CharacterRoleRecord, 0, len(rawRoles))
	for _, raw := range rawRoles {
		sourceIDs := rawRoleSourceEntityIDs(raw)
		if len(sourceIDs) == 0 {
			return nil, fmt.Errorf("character role missing source_entity_ids")
		}
		allowedScenes := make(map[string]bool)
		var sourceEntities []principalSourceEntity
		for _, entityID := range sourceIDs {
			entity, ok := input.EntityByID[entityID]
			if !ok {
				return nil, fmt.Errorf("character role references unknown source_entity_id %q", entityID)
			}
			if seenSourceIDs[entityID] {
				return nil, fmt.Errorf("source_entity_id %q appears in more than one character role", entityID)
			}
			seenSourceIDs[entityID] = true
			sourceEntities = append(sourceEntities, entity)
			for sceneID := range input.SourceScenesByID[entityID] {
				allowedScenes[sceneID] = true
			}
		}

		classification, err := normalizeCharacterClassification(string(raw.Classification))
		if err != nil {
			return nil, err
		}
		rationale := strings.TrimSpace(string(raw.Rationale))
		evidence, err := roleEvidenceFromRaw(raw.Evidence, classification, input.SceneIDs, allowedScenes)
		if err != nil {
			return nil, err
		}
		if classification == CharacterClassificationPrincipal {
			if rationale == "" {
				return nil, fmt.Errorf("principal character role for %s missing rationale", strings.Join(sourceIDs, ", "))
			}
			if len(evidence) == 0 {
				return nil, fmt.Errorf("principal character role for %s missing evidence", strings.Join(sourceIDs, ", "))
			}
		}

		sourceKey := characterRoleSourceKey(sourceIDs)
		characterID := existingIDs[sourceKey]
		if characterID == "" {
			characterID = ids.NewCharacterID()
		}
		records = append(records, CharacterRoleRecord{
			RecordType:      "character_role",
			CharacterID:     characterID,
			SourceEntityIDs: sortedCleanStrings(sourceIDs),
			CanonicalName:   chooseBookCharacterName(firstNonBlank(string(raw.CanonicalName), string(raw.Name)), sourceEntities),
			Aliases:         bookCharacterAliases(sourceEntities, firstNonBlank(string(raw.CanonicalName), string(raw.Name))),
			Classification:  classification,
			Role:            strings.TrimSpace(string(raw.Role)),
			Confidence:      defaultCharacterRoleConfidence(raw.Confidence),
			Rationale:       rationale,
			Evidence:        evidence,
			Generation:      generation,
			Status:          "generated",
		})
	}
	if len(seenSourceIDs) != len(input.SourceEntities) {
		missing := make([]string, 0)
		for _, entity := range input.SourceEntities {
			if !seenSourceIDs[entity.EntityID] {
				missing = append(missing, entity.EntityID)
			}
		}
		return nil, fmt.Errorf("principal response omitted source_entity_id(s): %s", strings.Join(missing, ", "))
	}
	sort.SliceStable(records, func(i, j int) bool {
		if strings.ToLower(records[i].CanonicalName) != strings.ToLower(records[j].CanonicalName) {
			return strings.ToLower(records[i].CanonicalName) < strings.ToLower(records[j].CanonicalName)
		}
		return records[i].CharacterID < records[j].CharacterID
	})
	return records, nil
}

func rawRoleSourceEntityIDs(raw rawCharacterRole) []string {
	ids := append([]string{}, []string(raw.SourceEntityIDs)...)
	ids = append(ids, []string(raw.EntityIDs)...)
	for _, value := range []string{string(raw.SourceEntityID), string(raw.EntityID)} {
		if strings.TrimSpace(value) != "" {
			ids = append(ids, value)
		}
	}
	return sortedCleanStrings(ids)
}

func normalizeCharacterClassification(value string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "", CharacterClassificationUncertain:
		return CharacterClassificationUncertain, nil
	case CharacterClassificationPrincipal, CharacterClassificationMajorSupporting, CharacterClassificationSupporting, CharacterClassificationMinor:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported character classification %q", value)
	}
}

func roleEvidenceFromRaw(raw []rawCharacterRoleEvidence, classification string, sceneIDs map[string]bool, allowedScenes map[string]bool) ([]CharacterRoleEvidence, error) {
	seen := make(map[string]bool, len(raw))
	out := make([]CharacterRoleEvidence, 0, len(raw))
	for _, item := range raw {
		sceneID := strings.TrimSpace(string(item.SceneID))
		reason := strings.TrimSpace(string(item.Reason))
		if sceneID == "" {
			continue
		}
		if !sceneIDs[sceneID] {
			return nil, fmt.Errorf("character role evidence references unknown scene_id %q", sceneID)
		}
		if !allowedScenes[sceneID] {
			return nil, fmt.Errorf("character role evidence scene %q is not linked to the role source entities", sceneID)
		}
		if reason == "" {
			return nil, fmt.Errorf("character role evidence for scene %s missing reason", sceneID)
		}
		key := sceneID + "\x00" + reason
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, CharacterRoleEvidence{SceneID: sceneID, Reason: reason})
	}
	if classification == CharacterClassificationPrincipal && len(out) == 0 {
		return nil, fmt.Errorf("principal character role missing evidence")
	}
	return out, nil
}

func chooseBookCharacterName(modelName string, sourceEntities []principalSourceEntity) string {
	modelName = strings.TrimSpace(modelName)
	for _, entity := range sourceEntities {
		if strings.EqualFold(modelName, entity.CanonicalName) {
			return entity.CanonicalName
		}
		for _, alias := range entity.Aliases {
			if strings.EqualFold(modelName, alias) {
				return alias
			}
		}
	}
	for _, entity := range sourceEntities {
		if strings.TrimSpace(entity.CanonicalName) != "" {
			return entity.CanonicalName
		}
	}
	return "Unnamed character"
}

func bookCharacterAliases(sourceEntities []principalSourceEntity, modelName string) []string {
	canonical := chooseBookCharacterName(modelName, sourceEntities)
	var aliases []string
	for _, entity := range sourceEntities {
		if strings.TrimSpace(entity.CanonicalName) != "" && !strings.EqualFold(entity.CanonicalName, canonical) {
			aliases = append(aliases, entity.CanonicalName)
		}
		aliases = append(aliases, entity.Aliases...)
	}
	return dedupeStringsExcluding(aliases, canonical)
}

func dedupeStringsExcluding(values []string, excluded string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.EqualFold(value, excluded) {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func defaultCharacterRoleConfidence(value float64) float64 {
	value = clampConfidence(value)
	if value == 0 {
		return 0.5
	}
	return value
}

func existingCharacterIDsBySourceSet(records []CharacterRoleRecord) map[string]string {
	out := make(map[string]string, len(records))
	for _, record := range records {
		key := characterRoleSourceKey(record.SourceEntityIDs)
		if key != "" && strings.TrimSpace(record.CharacterID) != "" {
			out[key] = record.CharacterID
		}
	}
	return out
}

func characterRoleSourceKey(sourceIDs []string) string {
	return strings.Join(sortedCleanStrings(sourceIDs), "\x00")
}

func sortedCleanStrings(values []string) []string {
	out := dedupeStrings(values)
	sort.Strings(out)
	return out
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

func hashCharacterRoleRecords(records []CharacterRoleRecord) (string, error) {
	data, err := json.Marshal(records)
	if err != nil {
		return "", err
	}
	return "sha256:" + sha256Hex(data), nil
}

func commitCharacterRoles(path string, records []CharacterRoleRecord, snapshot CharacterRolesSnapshotRecord) error {
	f, err := openAppendJSONL(path)
	if err != nil {
		return err
	}
	defer f.Close()
	items := make([]any, 0, len(records)+1)
	for _, record := range records {
		items = append(items, record)
	}
	items = append(items, snapshot)
	return appendJSONLBatch(f, items)
}

// ReadLatestCharacterRoles reads the latest committed character-role snapshot from model/character_roles.jsonl.
func ReadLatestCharacterRoles(path string) ([]CharacterRoleRecord, *CharacterRolesSnapshotRecord, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read character roles: %w", err)
	}
	defer f.Close()

	var pending []CharacterRoleRecord
	var latest []CharacterRoleRecord
	var latestSnapshot *CharacterRolesSnapshotRecord
	sc := bufioScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var typed struct {
			RecordType string `json:"record_type"`
		}
		if err := json.Unmarshal([]byte(line), &typed); err != nil {
			return nil, nil, fmt.Errorf("read character roles: malformed json: %w", err)
		}
		switch typed.RecordType {
		case "character_role":
			var rec CharacterRoleRecord
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				return nil, nil, fmt.Errorf("read character roles: malformed character_role: %w", err)
			}
			pending = append(pending, rec)
		case "character_roles_snapshot":
			var snap CharacterRolesSnapshotRecord
			if err := json.Unmarshal([]byte(line), &snap); err != nil {
				return nil, nil, fmt.Errorf("read character roles: malformed character_roles_snapshot: %w", err)
			}
			if snap.RoleCount != len(pending) {
				return nil, nil, fmt.Errorf("read character roles: character_roles_snapshot role_count mismatch: declared %d, pending %d", snap.RoleCount, len(pending))
			}
			latest = append([]CharacterRoleRecord(nil), pending...)
			snapCopy := snap
			latestSnapshot = &snapCopy
			pending = nil
		default:
			// Ignore unknown records for forward compatibility.
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("read character roles: %w", err)
	}
	return latest, latestSnapshot, nil
}

func bufioScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	return sc
}

func characterRolesSnapshotIsCurrent(snapshot *CharacterRolesSnapshotRecord, inputHash, model, promptVersion string) bool {
	if snapshot == nil {
		return false
	}
	return strings.TrimSpace(snapshot.InputHash) == strings.TrimSpace(inputHash) &&
		strings.TrimSpace(snapshot.Model) == strings.TrimSpace(model) &&
		strings.TrimSpace(snapshot.PromptVersion) == strings.TrimSpace(promptVersion) &&
		strings.TrimSpace(snapshot.ArtifactHash) != ""
}

func characterRoleTaskTypeForAttempt(attempt int) string {
	if attempt > 1 {
		return principalCharacterRetryTaskType
	}
	return principalCharacterTaskType
}

func recordCharacterRoleTask(run *Run, taskID, taskType, status, promptVersion, errMsg string, timings ...taskTiming) {
	if run == nil {
		return
	}
	if strings.TrimSpace(taskType) == "" {
		taskType = principalCharacterTaskType
	}
	record := TaskRecord{
		TaskID:        taskID,
		RunID:         runID(run),
		TaskType:      taskType,
		Status:        status,
		PromptVersion: promptVersion,
		Error:         errMsg,
	}
	if len(timings) > 0 {
		timings[0].applyTo(&record)
	}
	_ = run.recordTask(record)
}
