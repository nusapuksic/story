package compiler_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/store"
)

func TestCompilePrincipalsWithFakeProvider(t *testing.T) {
	p, st := buildTestProject(t)
	scenes := seedEntityReverseIndex(t, p, st)
	entityProvider := &fakeProvider{response: `{"entities":[{"canonical_name":"Mara","type":"character","aliases":["Maraa"],"occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara","Maraa"],"confidence":0.95}]}]}`}
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: entityProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities: %v", err)
	}
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}
	if len(paragraphs) == 0 {
		t.Fatal("expected paragraphs")
	}
	citedSummary := "Mara meets Old Petar [" + paragraphs[0].ID + "], chooses silence (" + paragraphs[0].ID + "), and remembers the lake."
	rawCard := `{"record_type":"scene_card","scene_id":"` + scenes[0].ID + `","title":"Bell warning","summary":"` + citedSummary + `","entities":["Mara","Maraa"],"pov":["Mara"],"participants":["Mara","Old Petar"],"evidence":["` + paragraphs[0].ID + `"],"generation":{},"status":"generated"}`
	if err := st.InsertSceneCard(store.SceneCardRow{
		SceneID: scenes[0].ID,
		Title:   "Bell warning",
		Summary: citedSummary,
		Evidence: []string{
			paragraphs[0].ID,
		},
		GenerationRun:   "compile-test",
		GenerationModel: "test-model",
		PromptVersion:   "scene-extraction-v1",
		Status:          "generated",
		RawJSON:         rawCard,
	}); err != nil {
		t.Fatalf("InsertSceneCard with cited summary: %v", err)
	}

	principalProvider := &fakeProvider{responseFunc: func(req provider.GenerationRequest, idx int) string {
		return principalRoleResponseFromPrompt(t, req.Messages[1].Content)
	}}
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerPrincipals,
		ExtractionProvider: principalProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile principals: %v", err)
	}
	if result.PrincipalsBuilt != 1 {
		t.Fatalf("PrincipalsBuilt = %d, want 1", result.PrincipalsBuilt)
	}
	prompt := principalProvider.requests[0].Messages[1].Content
	for _, want := range []string{
		"Use supplied canonical character entities as candidates.",
		"Do not redo entity resolution from scene text",
		"evidence.scene_id must be in allowed_scene_ids",
		"; allowed_scene_ids: " + scenes[0].ID,
		"Scene refs:",
		"summary: Mara meets Old Petar, chooses silence, and remembers the lake.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("principal prompt missing compact context %q:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{paragraphs[0].ID, "\n  - scene ", " surfaces: ", " fields: ", "; pov scenes: ", "; participant scenes: ", "; mentioned scenes: "} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("principal prompt contains redundant or unsanitized detail %q:\n%s", unwanted, prompt)
		}
	}
	assertRunPendingFiles(t, p, result.RunID, compiler.LayerPrincipals, 1)
	assertRunCommitLogSequences(t, p, result.RunID, compiler.LayerPrincipals, []int{0})

	rolesPath := p.Path(filepath.Join(project.ModelDir, "character_roles.jsonl"))
	data, err := os.ReadFile(rolesPath)
	if err != nil {
		t.Fatalf("read character_roles.jsonl: %v", err)
	}
	content := string(data)
	for _, want := range []string{`"record_type":"character_role"`, `"character_id":"char-`, `"classification":"principal"`, `"record_type":"character_roles_snapshot"`} {
		if !strings.Contains(content, want) {
			t.Fatalf("character_roles.jsonl missing %s:\n%s", want, content)
		}
	}

	second, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerPrincipals,
		ExtractionProvider: principalProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("second compile principals: %v", err)
	}
	if second.PrincipalsBuilt != 0 {
		t.Fatalf("second PrincipalsBuilt = %d, want 0 current skip", second.PrincipalsBuilt)
	}
	if len(principalProvider.requests) != 1 {
		t.Fatalf("principal provider calls = %d, want 1 after current skip", len(principalProvider.requests))
	}
}

func TestCompilePrincipalsRetriesInvalidSourceEntityID(t *testing.T) {
	p, st := buildTestProject(t)
	scenes := seedEntityReverseIndex(t, p, st)
	entityProvider := &fakeProvider{response: `{"entities":[{"canonical_name":"Mara","type":"character","aliases":["Maraa"],"occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara","Maraa"],"confidence":0.95}]}]}`}
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: entityProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities: %v", err)
	}

	principalProvider := &fakeProvider{responseFunc: func(req provider.GenerationRequest, idx int) string {
		if idx == 0 {
			return `{"characters":[{"source_entity_ids":["entity-not-in-prompt"],"classification":"principal","role":"protagonist","confidence":0.94,"rationale":"Mara drives the central action.","evidence":[{"scene_id":"` + scenes[0].ID + `","reason":"Shows Mara carrying the scene action."}]}]}`
		}
		return principalRoleResponseFromPrompt(t, req.Messages[1].Content)
	}}
	var events []compiler.ProgressEvent
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerPrincipals,
		ExtractionProvider: principalProvider,
		ExtractionModel:    "fake-model",
		Progress: func(event compiler.ProgressEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("compile principals with retry: %v", err)
	}
	if result.PrincipalsBuilt != 1 {
		t.Fatalf("PrincipalsBuilt = %d, want 1", result.PrincipalsBuilt)
	}
	if len(principalProvider.requests) != 2 {
		t.Fatalf("principal provider calls = %d, want 2", len(principalProvider.requests))
	}
	if len(principalProvider.requests[1].Messages) < 3 {
		t.Fatalf("retry request messages = %d, want corrective user message", len(principalProvider.requests[1].Messages))
	}
	if !progressEventsContain(events, compiler.LayerPrincipals, "item-retry", `unknown source_entity_id "entity-not-in-prompt"`) {
		t.Fatalf("progress events missing principal retry reason: %#v", events)
	}
	retryPrompt := principalProvider.requests[1].Messages[2].Content
	for _, want := range []string{
		"Return a complete replacement JSON object, not a patch.",
		"Use the source_entity_id and allowed_scene_ids from the previous prompt.",
		"Every evidence.scene_id must belong to a source_entity_id in the same role.",
	} {
		if !strings.Contains(retryPrompt, want) {
			t.Fatalf("retry prompt missing %q:\n%s", want, retryPrompt)
		}
	}
	for _, unwanted := range []string{"entity-not-in-prompt", scenes[0].ID, "Role refs:"} {
		if strings.Contains(retryPrompt, unwanted) {
			t.Fatalf("retry prompt should not repeat %q:\n%s", unwanted, retryPrompt)
		}
	}

	runDir := p.Path(filepath.Join(project.RunsDir, result.RunID))
	tasks, err := os.ReadFile(filepath.Join(runDir, "tasks.jsonl"))
	if err != nil {
		t.Fatalf("read tasks.jsonl: %v", err)
	}
	for _, want := range []string{
		`"task_type":"principal-characters"`,
		`"task_type":"principal-characters-retry"`,
		`"status":"failed"`,
		`"status":"completed"`,
	} {
		if !strings.Contains(string(tasks), want) {
			t.Fatalf("tasks missing %s:\n%s", want, tasks)
		}
	}
	summary, err := os.ReadFile(filepath.Join(runDir, "summary.json"))
	if err != nil {
		t.Fatalf("read summary.json: %v", err)
	}
	for _, want := range []string{`"total_provider_calls": 2`, `"retry_tasks": 1`} {
		if !strings.Contains(string(summary), want) {
			t.Fatalf("summary missing %s:\n%s", want, summary)
		}
	}
}

func TestCompilePrincipalsRetriesEvidenceSceneNotLinkedToSourceEntity(t *testing.T) {
	p, st := buildTestProject(t)
	scenes := seedEntityReverseIndex(t, p, st)
	seedSecondSceneCrewReverseIndex(t, p, st, scenes)
	entityProvider := &fakeProvider{response: `{"entities":[{"canonical_name":"Mara","type":"character","aliases":["Maraa"],"occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara","Maraa"],"confidence":0.95}]},{"canonical_name":"Crew","type":"character","aliases":["good-for-nothing crew"],"occurrences":[{"scene_id":"` + scenes[1].ID + `","surface_texts":["good-for-nothing crew"],"confidence":0.9}]}]}`}
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: entityProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities: %v", err)
	}

	principalProvider := &fakeProvider{responseFunc: func(req provider.GenerationRequest, idx int) string {
		prompt := req.Messages[1].Content
		maraID := promptEntityIDForName(t, prompt, "Mara")
		crewID := promptEntityIDForName(t, prompt, "Crew")
		maraScene := promptAllowedSceneForName(t, prompt, "Mara")
		crewScene := promptAllowedSceneForName(t, prompt, "Crew")
		if idx == 0 {
			return `{"characters":[{"source_entity_ids":["` + maraID + `"],"classification":"principal","role":"protagonist","confidence":0.94,"rationale":"Mara drives the central action.","evidence":[{"scene_id":"` + crewScene + `","reason":"Wrongly cites the crew scene for Mara."}]},{"source_entity_ids":["` + crewID + `"],"classification":"supporting","role":"antagonistic group","confidence":0.7,"rationale":"The crew pressures Mara.","evidence":[]}]}`
		}
		return `{"characters":[{"source_entity_ids":["` + maraID + `"],"classification":"principal","role":"protagonist","confidence":0.94,"rationale":"Mara drives the central action.","evidence":[{"scene_id":"` + maraScene + `","reason":"Shows Mara carrying the scene action."}]},{"source_entity_ids":["` + crewID + `"],"classification":"supporting","role":"antagonistic group","confidence":0.7,"rationale":"The crew pressures Mara.","evidence":[]}]}`
	}}
	var events []compiler.ProgressEvent
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerPrincipals,
		ExtractionProvider: principalProvider,
		ExtractionModel:    "fake-model",
		Progress: func(event compiler.ProgressEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("compile principals with linked-scene retry: %v", err)
	}
	if result.PrincipalsBuilt != 2 {
		t.Fatalf("PrincipalsBuilt = %d, want 2", result.PrincipalsBuilt)
	}
	if len(principalProvider.requests) != 2 {
		t.Fatalf("principal provider calls = %d, want 2", len(principalProvider.requests))
	}
	if !progressEventsContain(events, compiler.LayerPrincipals, "item-retry", "is not linked to the role source entities") {
		t.Fatalf("progress events missing linked-scene retry reason: %#v", events)
	}
	retryPrompt := principalProvider.requests[1].Messages[2].Content
	for _, want := range []string{
		"Return a complete replacement JSON object, not a patch.",
		"Use the source_entity_id and allowed_scene_ids from the previous prompt.",
		"Every evidence.scene_id must belong to a source_entity_id in the same role.",
	} {
		if !strings.Contains(retryPrompt, want) {
			t.Fatalf("retry prompt missing %q:\n%s", want, retryPrompt)
		}
	}
	for _, unwanted := range []string{scenes[0].ID, scenes[1].ID, "Role refs:", "name: Mara", "name: Crew"} {
		if strings.Contains(retryPrompt, unwanted) {
			t.Fatalf("retry prompt should not repeat %q:\n%s", unwanted, retryPrompt)
		}
	}
}
func TestCompilePrincipalsFailsAfterRetryingInvalidSourceEntityID(t *testing.T) {
	p, st := buildTestProject(t)
	scenes := seedEntityReverseIndex(t, p, st)
	entityProvider := &fakeProvider{response: `{"entities":[{"canonical_name":"Mara","type":"character","aliases":["Maraa"],"occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara","Maraa"],"confidence":0.95}]}]}`}
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: entityProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities: %v", err)
	}

	badProvider := &fakeProvider{response: `{"characters":[{"source_entity_ids":["entity-not-in-prompt"],"classification":"principal","role":"protagonist","confidence":0.94,"rationale":"Mara drives the central action.","evidence":[{"scene_id":"` + scenes[0].ID + `","reason":"Shows Mara carrying the scene action."}]}]}`}
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerPrincipals,
		ExtractionProvider: badProvider,
		ExtractionModel:    "fake-model",
	})
	if err == nil {
		t.Fatal("expected principals compile to fail after retry attempts")
	}
	if !strings.Contains(err.Error(), "failed after 3 attempts") || !strings.Contains(err.Error(), `unknown source_entity_id "entity-not-in-prompt"`) {
		t.Fatalf("error = %v, want exhausted retry detail with unknown source_entity_id", err)
	}
	if len(badProvider.requests) != 3 {
		t.Fatalf("principal provider calls = %d, want 3", len(badProvider.requests))
	}

	tasks, err := os.ReadFile(p.Path(filepath.Join(project.RunsDir, result.RunID, "tasks.jsonl")))
	if err != nil {
		t.Fatalf("read tasks.jsonl: %v", err)
	}
	if got := strings.Count(string(tasks), `"task_type":"principal-characters-retry"`); got != 2 {
		t.Fatalf("retry task count = %d, want 2\n%s", got, tasks)
	}
	if got := strings.Count(string(tasks), `"status":"failed"`); got != 3 {
		t.Fatalf("failed task count = %d, want 3\n%s", got, tasks)
	}

	rolesPath := p.Path(filepath.Join(project.ModelDir, "character_roles.jsonl"))
	data, err := os.ReadFile(rolesPath)
	if err != nil {
		t.Fatalf("read character_roles.jsonl: %v", err)
	}
	if strings.Contains(string(data), `"record_type":"character_roles_snapshot"`) {
		t.Fatalf("invalid principals output should not commit snapshot:\n%s", data)
	}
}

func TestCompilePrincipalsRejectsAliasAsSourceCandidate(t *testing.T) {
	p, st := buildTestProject(t)
	scenes := seedEntityReverseIndex(t, p, st)
	entityProvider := &fakeProvider{response: `{"entities":[{"canonical_name":"Mara","type":"character","aliases":["Maraa"],"occurrences":[{"scene_id":"` + scenes[0].ID + `","surface_texts":["Mara","Maraa"],"confidence":0.95}]}]}`}
	_, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerEntities,
		ExtractionProvider: entityProvider,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile entities: %v", err)
	}

	aliasProvider := &fakeProvider{response: `{"characters":[{"source_entity_ids":["Maraa"],"classification":"principal","rationale":"Alias was incorrectly treated as a candidate.","evidence":[{"scene_id":"` + scenes[0].ID + `","reason":"Bad alias evidence."}]}]}`}
	_, err = compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerPrincipals,
		ExtractionProvider: aliasProvider,
		ExtractionModel:    "fake-model",
	})
	if err == nil {
		t.Fatal("expected alias source candidate to be rejected")
	}
	if !strings.Contains(err.Error(), `unknown source_entity_id "Maraa"`) {
		t.Fatalf("error = %v, want unknown source_entity_id for alias", err)
	}

	rolesPath := p.Path(filepath.Join(project.ModelDir, "character_roles.jsonl"))
	data, err := os.ReadFile(rolesPath)
	if err != nil {
		t.Fatalf("read character_roles.jsonl: %v", err)
	}
	if strings.Contains(string(data), `"record_type":"character_roles_snapshot"`) {
		t.Fatalf("invalid principals output should not commit snapshot:\n%s", data)
	}
}
