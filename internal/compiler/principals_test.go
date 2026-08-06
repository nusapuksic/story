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
		"Use the supplied canonical character entity records as candidates.",
		"Do not redo entity resolution from scene text",
		"; linked scenes: " + scenes[0].ID,
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
