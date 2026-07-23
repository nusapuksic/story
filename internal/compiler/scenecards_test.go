package compiler_test

import (
	"testing"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/store"
)

func TestParseSceneCardResponseValid(t *testing.T) {
	raw := `{
		"title": "Mara hides the letter",
		"summary": "Mara receives a letter and hides it.",
		"pov": [],
		"participants": [],
		"locations": [],
		"evidence": ["p-001", "p-002"]
	}`
	pidSet := map[string]bool{"p-001": true, "p-002": true}

	card, err := compiler.ParseSceneCardResponseForTest(raw, "sc-001", pidSet, "run-001", "test-model")
	if err != nil {
		t.Fatalf("ParseSceneCardResponseForTest error = %v", err)
	}
	if card.Title != "Mara hides the letter" {
		t.Errorf("Title = %q", card.Title)
	}
	if card.SceneID != "sc-001" {
		t.Errorf("SceneID = %q", card.SceneID)
	}
	if len(card.Evidence) != 2 {
		t.Errorf("Evidence len = %d, want 2", len(card.Evidence))
	}
	if card.Status != "generated" {
		t.Errorf("Status = %q, want generated", card.Status)
	}
	if card.Generation.PromptVersion != "scene-extraction-v1" {
		t.Errorf("PromptVersion = %q", card.Generation.PromptVersion)
	}
}

func TestParseSceneCardResponseUnknownParagraphID(t *testing.T) {
	raw := `{
		"title": "Test",
		"summary": "Summary.",
		"evidence": ["p-UNKNOWN"]
	}`
	pidSet := map[string]bool{"p-001": true}
	_, err := compiler.ParseSceneCardResponseForTest(raw, "sc-001", pidSet, "run-001", "model")
	if err == nil {
		t.Fatal("expected error for unknown paragraph ID in evidence")
	}
}

func TestParseSceneCardResponseMissingTitle(t *testing.T) {
	raw := `{"summary": "Mara receives a letter and hides it.", "evidence": []}`
	pidSet := map[string]bool{}
	card, err := compiler.ParseSceneCardResponseForTest(raw, "sc-001", pidSet, "run-001", "model")
	if err != nil {
		t.Fatalf("expected missing title to be derived, got %v", err)
	}
	if card.Title != "Mara receives a letter and hides it" {
		t.Errorf("Title = %q", card.Title)
	}
}

func TestParseSceneCardResponseObjectSummary(t *testing.T) {
	raw := `{
		"scene_cards": [{"id": "card_1", "action": "Mara finds a letter."}],
		"summary": {
			"plot_overview": "Mara finds the letter and hides it.",
			"themes": ["secrecy", "fear"]
		},
		"participants": [{"name": "Mara", "role": "protagonist"}],
		"evidence": []
	}`
	pidSet := map[string]bool{}
	card, err := compiler.ParseSceneCardResponseForTest(raw, "sc-001", pidSet, "run-001", "model")
	if err != nil {
		t.Fatalf("expected object summary to be coerced, got %v", err)
	}
	if card.Summary != "Mara finds the letter and hides it." {
		t.Errorf("Summary = %q", card.Summary)
	}
	if card.Title != "Mara finds the letter and hides it" {
		t.Errorf("Title = %q", card.Title)
	}
	if len(card.Participants) != 1 || card.Participants[0] != "Mara" {
		t.Errorf("Participants = %v", card.Participants)
	}
}

func TestParseSceneCardResponseMissingSummary(t *testing.T) {
	raw := `{"title": "Mara hides the letter", "evidence": []}`
	pidSet := map[string]bool{}
	card, err := compiler.ParseSceneCardResponseForTest(raw, "sc-001", pidSet, "run-001", "model")
	if err != nil {
		t.Fatalf("expected missing summary to be derived, got %v", err)
	}
	if card.Summary != "Mara hides the letter" {
		t.Errorf("Summary = %q", card.Summary)
	}
}

func TestParseSceneCardResponseMarkdownFence(t *testing.T) {
	raw := "```json\n{\"title\":\"T\",\"summary\":\"S.\",\"evidence\":[]}\n```"
	pidSet := map[string]bool{}
	card, err := compiler.ParseSceneCardResponseForTest(raw, "sc-001", pidSet, "run-001", "model")
	if err != nil {
		t.Fatalf("expected success with markdown fence, got %v", err)
	}
	if card.Title != "T" {
		t.Errorf("Title = %q, want T", card.Title)
	}
}

func TestParseSceneCardResponseMalformedJSON(t *testing.T) {
	raw := `not json at all`
	pidSet := map[string]bool{}
	_, err := compiler.ParseSceneCardResponseForTest(raw, "sc-001", pidSet, "run-001", "model")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// TestCompileSceneCardWithFakeProvider exercises the full extractSceneCard
// path using a synthetic scene and paragraph set.
func TestExtractSceneCardWithFakeProvider(t *testing.T) {
	paragraphs := []store.ParagraphRow{
		{ID: "p-A", ChapterID: "ch-0001", Ordinal: 1, Text: "She found the letter."},
		{ID: "p-B", ChapterID: "ch-0001", Ordinal: 2, Text: "She hid it under the stove."},
	}
	scene := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-A",
		ParagraphEnd:   "p-B",
		BoundarySource: "explicit",
	}
	responseJSON := `{"title":"She hides the letter","summary":"The protagonist hides a letter.","evidence":["p-A","p-B"]}`

	fake := &fakeProvider{response: responseJSON}
	card, err := compiler.ExtractSceneCardForTest(fake, scene, paragraphs, "test-model")
	if err != nil {
		t.Fatalf("ExtractSceneCardForTest error = %v", err)
	}
	if card.Title != "She hides the letter" {
		t.Errorf("Title = %q", card.Title)
	}
	if len(card.Evidence) != 2 {
		t.Errorf("Evidence = %v", card.Evidence)
	}
}

func TestExtractSceneCardMissingTitleAndSummaryUsesSceneText(t *testing.T) {
	paragraphs := []store.ParagraphRow{
		{ID: "p-A", ChapterID: "ch-0001", Ordinal: 1, Text: "She found the letter. She hid it under the stove."},
	}
	scene := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-A",
		ParagraphEnd:   "p-A",
	}
	fake := &fakeProvider{response: `{"evidence": []}`}

	card, err := compiler.ExtractSceneCardForTest(fake, scene, paragraphs, "test-model")
	if err != nil {
		t.Fatalf("expected missing title and summary to be derived, got %v", err)
	}
	if card.Summary != "She found the letter." {
		t.Errorf("Summary = %q", card.Summary)
	}
	if card.Title != "She found the letter" {
		t.Errorf("Title = %q", card.Title)
	}
}

func TestExtractSceneCardTruncatedJSONUsesSceneText(t *testing.T) {
	paragraphs := []store.ParagraphRow{
		{ID: "p-A", ChapterID: "ch-0001", Ordinal: 1, Text: "Mara accepts the key and enters the lower vault. The guard waits outside."},
	}
	scene := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-A",
		ParagraphEnd:   "p-A",
	}
	fake := &fakeProvider{response: `{"scene_card":{"plot_summary":"Mara accepts the`}

	card, err := compiler.ExtractSceneCardForTest(fake, scene, paragraphs, "test-model")
	if err != nil {
		t.Fatalf("expected truncated JSON to fall back to scene text, got %v", err)
	}
	if card.Summary != "Mara accepts the key and enters the lower vault." {
		t.Errorf("Summary = %q", card.Summary)
	}
	if card.Title != "Mara accepts the key and enters the lower vault" {
		t.Errorf("Title = %q", card.Title)
	}
	if len(card.Evidence) != 1 || card.Evidence[0] != "p-A" {
		t.Errorf("Evidence = %v", card.Evidence)
	}
}

func TestExtractSceneCardInvalidEvidence(t *testing.T) {
	paragraphs := []store.ParagraphRow{
		{ID: "p-A", ChapterID: "ch-0001", Ordinal: 1, Text: "She found the letter."},
	}
	scene := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-A",
		ParagraphEnd:   "p-A",
	}
	// LLM returns a paragraph ID that does not exist in the scene.
	responseJSON := `{"title":"T","summary":"S.","evidence":["p-NONEXISTENT"]}`
	fake := &fakeProvider{response: responseJSON}
	_, err := compiler.ExtractSceneCardForTest(fake, scene, paragraphs, "test-model")
	if err == nil {
		t.Fatal("expected error for unknown evidence paragraph ID")
	}
}
