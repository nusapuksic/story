package compiler

import (
	"context"
	"encoding/json"
	"fmt"
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

var entityResolutionTermTypes = []string{
	store.ReverseTermEntity,
	store.ReverseTermParticipant,
	store.ReverseTermPOV,
	store.ReverseTermLocation,
}

// EntityRecord represents one consolidated entity in model/entities.jsonl.
type EntityRecord struct {
	RecordType    string             `json:"record_type"`
	ID            string             `json:"id"`
	ChapterID     string             `json:"chapter_id"`
	Type          string             `json:"type"`
	CanonicalName string             `json:"canonical_name"`
	Aliases       []string           `json:"aliases,omitempty"`
	Evidence      []string           `json:"evidence"`
	Occurrences   []EntityOccurrence `json:"occurrences,omitempty"`
	Flags         []EntityFlag       `json:"flags,omitempty"`
	Generation    EntityGeneration   `json:"generation"`
	Status        string             `json:"status"`
}

// EntityOccurrence is the scene-scoped occurrence shape embedded in entity records.
type EntityOccurrence struct {
	ChapterID    string       `json:"chapter_id"`
	SceneID      string       `json:"scene_id"`
	SurfaceTexts []string     `json:"surface_texts"`
	SourceFields []string     `json:"source_fields"`
	Confidence   float64      `json:"confidence"`
	Flags        []EntityFlag `json:"flags,omitempty"`
}

// OccurrenceRecord is one scene-scoped entity occurrence in model/occurrences.jsonl.
type OccurrenceRecord struct {
	RecordType   string           `json:"record_type"`
	EntityID     string           `json:"entity_id"`
	ChapterID    string           `json:"chapter_id"`
	SceneID      string           `json:"scene_id"`
	SurfaceTexts []string         `json:"surface_texts"`
	SourceFields []string         `json:"source_fields"`
	Confidence   float64          `json:"confidence"`
	Flags        []EntityFlag     `json:"flags,omitempty"`
	Generation   EntityGeneration `json:"generation"`
	Status       string           `json:"status"`
}

// EntityFlag marks review-worthy consolidation decisions, such as likely typos.
type EntityFlag struct {
	Type       string  `json:"type"`
	Value      string  `json:"value,omitempty"`
	Suggested  string  `json:"suggested,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

// EntityGeneration is provenance for generated entity records.
type EntityGeneration struct {
	RunID         string `json:"run_id"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
	GeneratedAt   string `json:"generated_at"`
}

// EntitySnapshotRecord marks that all entity and occurrence records for a
// chapter have been fully written to model/entities.jsonl and
// model/occurrences.jsonl.
type EntitySnapshotRecord struct {
	RecordType      string `json:"record_type"` // "entity_snapshot"
	ChapterID       string `json:"chapter_id"`
	EntityCount     int    `json:"entity_count"`
	OccurrenceCount int    `json:"occurrence_count"`
	CommittedAt     string `json:"committed_at"` // RFC3339
}

type rawEntityResponse struct {
	Entities []rawEntityCandidate `json:"entities"`
}

type rawEntityCandidate struct {
	CanonicalName flexibleString     `json:"canonical_name"`
	Type          flexibleString     `json:"type"`
	Aliases       flexibleStringList `json:"aliases"`
	Occurrences   []rawOccurrence    `json:"occurrences"`
	Flags         []rawEntityFlag    `json:"flags"`
}

type rawOccurrence struct {
	SceneID      flexibleString     `json:"scene_id"`
	SurfaceTexts flexibleStringList `json:"surface_texts"`
	SurfaceText  flexibleString     `json:"surface_text"`
	Confidence   float64            `json:"confidence"`
	Flags        []rawEntityFlag    `json:"flags"`
}

type rawEntityFlag struct {
	Type       flexibleString `json:"type"`
	Value      flexibleString `json:"value"`
	Suggested  flexibleString `json:"suggested"`
	Reason     flexibleString `json:"reason"`
	Confidence float64        `json:"confidence"`
}

// compileEntities runs scene-card reverse-index consolidation for requested chapters.
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

	occurrencesFile, err := openAppendJSONL(p.Path(filepath.Join(project.ModelDir, "occurrences.jsonl")))
	if err != nil {
		return 0, err
	}
	defer occurrencesFile.Close()
	committer := compileArtifactCommitter{st: st, staging: staging, entitiesFile: entitiesFile, occurrencesFile: occurrencesFile}

	items := make([]OrderedWorkItem[entityWorkInput], 0, len(chapters))
	for chapterIndex, ch := range chapters {
		if !opts.Force {
			committed, err := st.IsEntitySnapshotCommitted(ch.ID)
			if err != nil {
				return 0, err
			}
			if committed {
				reportProgress(opts, ProgressEvent{Layer: LayerEntities, Stage: "item-skip", ChapterID: ch.ID, Current: chapterIndex + 1, Total: len(chapters), Message: fmt.Sprintf("Entities %s (%d/%d): already current", ch.ID, chapterIndex+1, len(chapters))})
				continue
			}
		}

		refs, err := st.ReverseIndexRefsForChapter(ch.ID, entityResolutionTermTypes)
		if err != nil {
			return 0, err
		}
		items = append(items, OrderedWorkItem[entityWorkInput]{
			Sequence: len(items),
			TaskID:   ch.ID,
			Input: entityWorkInput{
				Chapter:      ch,
				ChapterIndex: chapterIndex,
				ChapterTotal: len(chapters),
				Refs:         refs,
				Force:        opts.Force,
			},
		})
	}

	total := 0
	err = RunOrderedWork(ctx, items, OrderedExecutorOptions{WorkerLimit: 1}, func(ctx context.Context, item OrderedWorkItem[entityWorkInput]) (entityWorkOutput, error) {
		input := item.Input
		output := entityWorkOutput{Input: input}
		if len(input.Refs) > 0 {
			candidates, err := consolidateEntitiesForChapter(ctx, p, input.Chapter, input.Refs,
				opts.ExtractionProvider, opts.ExtractionModel, cfg, run)
			if err != nil {
				return entityWorkOutput{}, err
			}
			output.Candidates = candidates
		}
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
		if len(input.Refs) == 0 {
			reportProgress(opts, ProgressEvent{Layer: LayerEntities, Stage: "item-start", ChapterID: input.Chapter.ID, Current: input.ChapterIndex + 1, Total: input.ChapterTotal, Message: fmt.Sprintf("Entities %s (%d/%d): no reverse-index candidates", input.Chapter.ID, input.ChapterIndex+1, input.ChapterTotal)})
		} else {
			reportProgress(opts, ProgressEvent{Layer: LayerEntities, Stage: "item-start", ChapterID: input.Chapter.ID, Current: input.ChapterIndex + 1, Total: input.ChapterTotal, Message: fmt.Sprintf("Entities %s (%d/%d): consolidating %d reverse-index candidate(s)", input.Chapter.ID, input.ChapterIndex+1, input.ChapterTotal, len(input.Refs))})
		}
		entities, occurrences := finalizeEntityCandidates(input.Chapter.ID, output.Candidates)
		output.Snapshot = entitySnapshotForRecords(input.Chapter.ID, entities, occurrences)
		if err := committer.CommitEntities(output, entities, occurrences); err != nil {
			return err
		}
		total += len(entities)
		reportProgress(opts, ProgressEvent{Layer: LayerEntities, Stage: "item-complete", ChapterID: input.Chapter.ID, Current: input.ChapterIndex + 1, Total: input.ChapterTotal, Message: fmt.Sprintf("Entities %s (%d/%d): completed (%d entities, %d occurrences)", input.Chapter.ID, input.ChapterIndex+1, input.ChapterTotal, len(entities), len(occurrences))})
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
		SchemaVersion: 2,
	}, stagedEntityPayload{Candidates: output.Candidates})
}

func finalizeEntityCandidates(chapterID string, candidates []entityRecordCandidate) ([]EntityRecord, []OccurrenceRecord) {
	entities := make([]EntityRecord, 0, len(candidates))
	var occurrences []OccurrenceRecord
	for _, candidate := range candidates {
		if len(candidate.Occurrences) == 0 {
			continue
		}
		entityID := ids.NewEntityID()
		entityOccurrences := make([]EntityOccurrence, 0, len(candidate.Occurrences))
		for _, occurrenceCandidate := range candidate.Occurrences {
			occ := OccurrenceRecord{
				RecordType:   "occurrence",
				EntityID:     entityID,
				ChapterID:    occurrenceCandidate.ChapterID,
				SceneID:      occurrenceCandidate.SceneID,
				SurfaceTexts: occurrenceCandidate.SurfaceTexts,
				SourceFields: occurrenceCandidate.SourceFields,
				Confidence:   occurrenceCandidate.Confidence,
				Flags:        occurrenceCandidate.Flags,
				Generation:   occurrenceCandidate.Generation,
				Status:       occurrenceCandidate.Status,
			}
			occurrences = append(occurrences, occ)
			entityOccurrences = append(entityOccurrences, EntityOccurrence{
				ChapterID:    occ.ChapterID,
				SceneID:      occ.SceneID,
				SurfaceTexts: occ.SurfaceTexts,
				SourceFields: occ.SourceFields,
				Confidence:   occ.Confidence,
				Flags:        occ.Flags,
			})
		}
		entity := EntityRecord{
			RecordType:    "entity",
			ID:            entityID,
			ChapterID:     chapterID,
			Type:          candidate.Type,
			CanonicalName: candidate.CanonicalName,
			Aliases:       candidate.Aliases,
			Evidence:      candidate.Evidence,
			Occurrences:   entityOccurrences,
			Flags:         candidate.Flags,
			Generation:    candidate.Generation,
			Status:        candidate.Status,
		}
		entities = append(entities, entity)
	}
	return entities, occurrences
}

func entityRowFromRecord(entity EntityRecord) store.EntityRow {
	rawBytes, _ := json.Marshal(entity)
	return store.EntityRow{
		ID:              entity.ID,
		ChapterID:       entity.ChapterID,
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

func entitySnapshotForRecords(chapterID string, entities []EntityRecord, occurrences []OccurrenceRecord) EntitySnapshotRecord {
	return EntitySnapshotRecord{
		RecordType:      "entity_snapshot",
		ChapterID:       chapterID,
		EntityCount:     len(entities),
		OccurrenceCount: uniqueOccurrenceRecordCount(occurrences),
		CommittedAt:     time.Now().UTC().Format(time.RFC3339),
	}
}

func uniqueOccurrenceRecordCount(occurrences []OccurrenceRecord) int {
	seen := make(map[string]bool, len(occurrences))
	for _, occurrence := range occurrences {
		key := occurrence.EntityID + "\x00" + occurrence.SceneID
		seen[key] = true
	}
	return len(seen)
}

func occurrenceRowFromRecord(occurrence OccurrenceRecord) store.OccurrenceRow {
	rawBytes, _ := json.Marshal(occurrence)
	return store.OccurrenceRow{
		EntityID:        occurrence.EntityID,
		ChapterID:       occurrence.ChapterID,
		SceneID:         occurrence.SceneID,
		SurfaceTexts:    occurrence.SurfaceTexts,
		SourceFields:    occurrence.SourceFields,
		Confidence:      occurrence.Confidence,
		GenerationRun:   occurrence.Generation.RunID,
		GenerationModel: occurrence.Generation.Model,
		PromptVersion:   occurrence.Generation.PromptVersion,
		Status:          occurrence.Status,
		RawJSON:         string(rawBytes),
	}
}

func consolidateEntitiesForChapter(
	ctx context.Context,
	p *project.Project,
	ch store.ChapterRow,
	refs []store.ReverseIndexRef,
	prov provider.Provider,
	model string,
	cfg sceneDetectConfig,
	run *Run,
) ([]entityRecordCandidate, error) {
	if prov == nil {
		return nil, fmt.Errorf("no LLM provider: cannot consolidate entities for %s", ch.ID)
	}
	loadedPrompt := loadCompilerPrompt(p, storyprompts.EntityResolution)
	prompt := buildEntityPrompt(ch, refs)
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
		return nil, fmt.Errorf("entity consolidation LLM call for %s: %w", ch.ID, err)
	}

	candidates, parseErr := parseEntityResponse(resp.Content, ch.ID, refs, runID(run), model, loadedPrompt.Version)
	status := TaskStatusCompleted
	errMsg := ""
	if parseErr != nil {
		status = TaskStatusFailed
		errMsg = parseErr.Error()
	}
	recordEntityTask(run, taskID, ch.ID, status, loadedPrompt.Version, errMsg, timing)
	return candidates, parseErr
}

func buildEntityPrompt(ch store.ChapterRow, refs []store.ReverseIndexRef) string {
	var sb strings.Builder
	sb.WriteString("Consolidate candidate entities from scene-card reverse-index results as JSON.\n")
	sb.WriteString("Chapter ID: ")
	sb.WriteString(ch.ID)
	sb.WriteString("\nTitle: ")
	sb.WriteString(ch.Title)
	sb.WriteString("\nReturn JSON matching the schema:\n")
	sb.WriteString(`{"entities":[{"canonical_name":"...","type":"character|location|object|organization|group|document|event-concept|unknown","aliases":[],"occurrences":[{"scene_id":"sc-...","surface_texts":["..."],"confidence":0.9,"flags":[{"type":"possible_typo","value":"...","suggested":"...","reason":"...","confidence":0.8}]}],"flags":[]}]}`)
	sb.WriteString("\nUse only scene IDs and surface_texts listed below. Occurrences are scene-scoped. ")
	sb.WriteString("Merge aliases conservatively; preserve ambiguity by keeping uncertain names separate. ")
	sb.WriteString("Flag likely typos instead of silently correcting original surface_texts.\n\n")
	sb.WriteString("Reverse-index candidates:\n")
	writeReverseIndexRefs(&sb, refs)
	return sb.String()
}

func writeReverseIndexRefs(sb *strings.Builder, refs []store.ReverseIndexRef) {
	byScene := make(map[string]map[string][]string)
	var scenes []string
	seenScene := make(map[string]bool)
	for _, ref := range refs {
		if strings.TrimSpace(ref.SceneID) == "" || strings.TrimSpace(ref.RawValue) == "" {
			continue
		}
		if !seenScene[ref.SceneID] {
			seenScene[ref.SceneID] = true
			scenes = append(scenes, ref.SceneID)
		}
		field := strings.TrimSpace(ref.SourceField)
		if field == "" {
			field = strings.TrimSpace(ref.TermType)
		}
		if byScene[ref.SceneID] == nil {
			byScene[ref.SceneID] = make(map[string][]string)
		}
		byScene[ref.SceneID][field] = append(byScene[ref.SceneID][field], ref.RawValue)
	}
	for _, sceneID := range scenes {
		sb.WriteString("- scene ")
		sb.WriteString(sceneID)
		sb.WriteString("\n")
		fields := make([]string, 0, len(byScene[sceneID]))
		for field := range byScene[sceneID] {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			values := dedupeStrings(byScene[sceneID][field])
			if len(values) == 0 {
				continue
			}
			sb.WriteString("  ")
			sb.WriteString(field)
			sb.WriteString(": ")
			sb.WriteString(strings.Join(values, "; "))
			sb.WriteString("\n")
		}
	}
}

func parseEntityResponse(content, chapterID string, refs []store.ReverseIndexRef, runID, model, promptVersion string) ([]entityRecordCandidate, error) {
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
	return entityCandidatesFromRaw(raw, chapterID, refs, runID, model, promptVersion, strictEvidence)
}

func entityCandidatesFromRaw(
	raw rawEntityResponse,
	chapterID string,
	refs []store.ReverseIndexRef,
	runID, model, promptVersion string,
	strictEvidence bool,
) ([]entityRecordCandidate, error) {
	refIndex := newEntityRefIndex(refs)
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
		aliases := dedupeStrings([]string(cand.Aliases))
		flags := entityFlagsFromRaw(cand.Flags)
		var occurrences []occurrenceRecordCandidate
		seenScenes := make(map[string]bool)
		for _, rawOccurrence := range cand.Occurrences {
			occ, ok, err := occurrenceCandidateFromRaw(rawOccurrence, chapterID, name, aliases, generation, refIndex, strictEvidence)
			if err != nil {
				return nil, err
			}
			if !ok || seenScenes[occ.SceneID] {
				continue
			}
			seenScenes[occ.SceneID] = true
			occurrences = append(occurrences, occ)
		}
		if len(occurrences) == 0 {
			continue
		}
		evidence := occurrenceSceneIDs(occurrences)
		candidates = append(candidates, entityRecordCandidate{
			Type:          normalizeEntityType(string(cand.Type)),
			CanonicalName: name,
			Aliases:       aliases,
			Evidence:      evidence,
			Occurrences:   occurrences,
			Flags:         flags,
			Generation:    generation,
			Status:        "generated",
		})
	}
	return candidates, nil
}

type entityRefIndex struct {
	refsByScene  map[string][]store.ReverseIndexRef
	termsByScene map[string]map[string][]store.ReverseIndexRef
}

func newEntityRefIndex(refs []store.ReverseIndexRef) entityRefIndex {
	idx := entityRefIndex{
		refsByScene:  make(map[string][]store.ReverseIndexRef),
		termsByScene: make(map[string]map[string][]store.ReverseIndexRef),
	}
	for _, ref := range refs {
		sceneID := strings.TrimSpace(ref.SceneID)
		term := strings.TrimSpace(ref.RawValue)
		if term == "" {
			term = strings.TrimSpace(ref.Term)
		}
		if sceneID == "" || term == "" {
			continue
		}
		ref.RawValue = term
		idx.refsByScene[sceneID] = append(idx.refsByScene[sceneID], ref)
		key := normalizeEntitySurface(term)
		if idx.termsByScene[sceneID] == nil {
			idx.termsByScene[sceneID] = make(map[string][]store.ReverseIndexRef)
		}
		idx.termsByScene[sceneID][key] = append(idx.termsByScene[sceneID][key], ref)
	}
	return idx
}

func occurrenceCandidateFromRaw(
	raw rawOccurrence,
	chapterID, canonicalName string,
	aliases []string,
	generation EntityGeneration,
	idx entityRefIndex,
	strictEvidence bool,
) (occurrenceRecordCandidate, bool, error) {
	sceneID := strings.TrimSpace(string(raw.SceneID))
	if sceneID == "" {
		return occurrenceRecordCandidate{}, false, nil
	}
	if len(idx.refsByScene[sceneID]) == 0 {
		if strictEvidence {
			return occurrenceRecordCandidate{}, false, fmt.Errorf("entity %q cites unknown scene ID %q", canonicalName, sceneID)
		}
		return occurrenceRecordCandidate{}, false, nil
	}

	surfaces := dedupeStrings([]string(raw.SurfaceTexts))
	if surface := strings.TrimSpace(string(raw.SurfaceText)); surface != "" {
		surfaces = dedupeStrings(append(surfaces, surface))
	}
	if len(surfaces) == 0 {
		surfaces = matchingSceneTerms(sceneID, append([]string{canonicalName}, aliases...), idx)
	}
	surfaces, sourceFields := filterOccurrenceSurfaces(sceneID, surfaces, idx)
	if len(surfaces) == 0 {
		if strictEvidence {
			return occurrenceRecordCandidate{}, false, fmt.Errorf("entity %q occurrence for scene %s has no listed surface_texts", canonicalName, sceneID)
		}
		return occurrenceRecordCandidate{}, false, nil
	}
	confidence := clampConfidence(raw.Confidence)
	if confidence == 0 {
		confidence = 0.8
	}
	return occurrenceRecordCandidate{
		ChapterID:    chapterID,
		SceneID:      sceneID,
		SurfaceTexts: surfaces,
		SourceFields: sourceFields,
		Confidence:   confidence,
		Flags:        entityFlagsFromRaw(raw.Flags),
		Generation:   generation,
		Status:       "generated",
	}, true, nil
}

func matchingSceneTerms(sceneID string, names []string, idx entityRefIndex) []string {
	var out []string
	for _, name := range names {
		key := normalizeEntitySurface(name)
		for _, ref := range idx.termsByScene[sceneID][key] {
			out = append(out, ref.RawValue)
		}
	}
	return dedupeStrings(out)
}

func filterOccurrenceSurfaces(sceneID string, surfaces []string, idx entityRefIndex) ([]string, []string) {
	var filtered []string
	var fields []string
	for _, surface := range surfaces {
		key := normalizeEntitySurface(surface)
		for _, ref := range idx.termsByScene[sceneID][key] {
			filtered = append(filtered, ref.RawValue)
			field := strings.TrimSpace(ref.SourceField)
			if field == "" {
				field = strings.TrimSpace(ref.TermType)
			}
			fields = append(fields, field)
		}
	}
	return dedupeStrings(filtered), dedupeStrings(fields)
}

func normalizeEntitySurface(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func occurrenceSceneIDs(occurrences []occurrenceRecordCandidate) []string {
	ids := make([]string, 0, len(occurrences))
	for _, occ := range occurrences {
		ids = append(ids, occ.SceneID)
	}
	ids = dedupeStrings(ids)
	sort.Strings(ids)
	return ids
}

func entityFlagsFromRaw(rawFlags []rawEntityFlag) []EntityFlag {
	flags := make([]EntityFlag, 0, len(rawFlags))
	for _, raw := range rawFlags {
		flag := EntityFlag{
			Type:       strings.TrimSpace(string(raw.Type)),
			Value:      strings.TrimSpace(string(raw.Value)),
			Suggested:  strings.TrimSpace(string(raw.Suggested)),
			Reason:     strings.TrimSpace(string(raw.Reason)),
			Confidence: clampConfidence(raw.Confidence),
		}
		if flag.Type == "" {
			continue
		}
		flags = append(flags, flag)
	}
	return flags
}

func clampConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
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
