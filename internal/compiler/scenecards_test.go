package compiler_test

import (
	"context"
	"fmt"
	"strings"
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

func TestParseSceneCardResponseObjectEvidenceItems(t *testing.T) {
	raw := `{
		"title": "Mara hides the letter",
		"summary": "Mara receives a letter and hides it.",
		"evidence": [
			{"paragraph_id": "p-001", "quote": "Mara receives a letter."},
			{"id": "p-002", "reason": "She hides it."}
		]
	}`
	pidSet := map[string]bool{"p-001": true, "p-002": true}

	card, err := compiler.ParseSceneCardResponseForTest(raw, "sc-001", pidSet, "run-001", "test-model")
	if err != nil {
		t.Fatalf("ParseSceneCardResponseForTest error = %v", err)
	}
	if got := card.Evidence; len(got) != 2 || got[0] != "p-001" || got[1] != "p-002" {
		t.Errorf("Evidence = %v, want [p-001 p-002]", got)
	}
}

func TestParseSceneCardResponseEvidenceObjectsWithIDs(t *testing.T) {
	raw := `{
		"title": "The Attic Discovery",
		"summary": "Mara finds a chest in the attic.",
		"evidence": [
			{"summary": "Mara returns to the lake.", "ids": ["p-001", "p-002"]},
			{"summary": "Mara finds a chest.", "ids": ["p-003"]}
		]
	}`
	pidSet := map[string]bool{"p-001": true, "p-002": true, "p-003": true}

	card, err := compiler.ParseSceneCardResponseForTest(raw, "sc-001", pidSet, "run-001", "test-model")
	if err != nil {
		t.Fatalf("ParseSceneCardResponseForTest error = %v", err)
	}
	if got := card.Evidence; len(got) != 3 || got[0] != "p-001" || got[1] != "p-002" || got[2] != "p-003" {
		t.Errorf("Evidence = %v, want [p-001 p-002 p-003]", got)
	}
}

func TestParseSceneCardResponseObjectEvidenceField(t *testing.T) {
	raw := `{
		"title": "Mara hides the letter",
		"summary": "Mara receives a letter and hides it.",
		"evidence": {"paragraphs": ["p-001", {"paragraph_id": "p-002"}]}
	}`
	pidSet := map[string]bool{"p-001": true, "p-002": true}

	card, err := compiler.ParseSceneCardResponseForTest(raw, "sc-001", pidSet, "run-001", "test-model")
	if err != nil {
		t.Fatalf("ParseSceneCardResponseForTest error = %v", err)
	}
	if got := card.Evidence; len(got) != 2 || got[0] != "p-001" || got[1] != "p-002" {
		t.Errorf("Evidence = %v, want [p-001 p-002]", got)
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

func TestExtractSceneCardTruncatedJSONSkipsInsteadOfFallback(t *testing.T) {
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
		t.Fatalf("expected truncated JSON to skip scene card, got %v", err)
	}
	if card.Status != compiler.SceneCardStatusSkipped {
		t.Fatalf("Status = %q, want skipped", card.Status)
	}
	if card.Title != "" || card.Summary != "" || len(card.Evidence) != 0 {
		t.Fatalf("skipped card should not contain fallback title/summary/evidence: %#v", card)
	}
	if card.Recovery == nil || card.Recovery.Action != "skipped" || card.Recovery.Attempts != 2 {
		t.Fatalf("Recovery = %#v, want skipped after 2 attempts", card.Recovery)
	}
	if !strings.Contains(card.Recovery.Reason, "truncated model JSON") {
		t.Fatalf("Recovery reason = %q, want truncated JSON", card.Recovery.Reason)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("Generate calls = %d, want 2", len(fake.requests))
	}
	retryPrompt := fake.requests[1].Messages[1].Content
	if !strings.Contains(retryPrompt, "incomplete JSON") {
		t.Fatalf("retry prompt missing incomplete JSON guidance:\n%s", retryPrompt)
	}
}

func TestExtractSceneCardInvalidEvidenceRetriesSuccessfully(t *testing.T) {
	paragraphs := []store.ParagraphRow{
		{ID: "p-A", ChapterID: "ch-0001", Ordinal: 1, Text: "She found the letter."},
	}
	scene := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-A",
		ParagraphEnd:   "p-A",
	}
	fake := &fakeProvider{responses: []string{
		`{"title":"T","summary":"S.","evidence":["p-NONEXISTENT"]}`,
		`{"title":"Corrected","summary":"She finds the letter.","evidence":["p-A"]}`,
	}}

	card, err := compiler.ExtractSceneCardForTest(fake, scene, paragraphs, "test-model")
	if err != nil {
		t.Fatalf("expected retry to recover invalid evidence, got %v", err)
	}
	if card.Title != "Corrected" {
		t.Fatalf("Title = %q, want Corrected", card.Title)
	}
	if len(card.Evidence) != 1 || card.Evidence[0] != "p-A" {
		t.Fatalf("Evidence = %v, want [p-A]", card.Evidence)
	}
	if card.Recovery == nil || card.Recovery.Action != "retry" || card.Recovery.Attempts != 2 {
		t.Fatalf("Recovery = %#v, want retry after 2 attempts", card.Recovery)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("Generate calls = %d, want 2", len(fake.requests))
	}
	retryPrompt := fake.requests[1].Messages[1].Content
	if !strings.Contains(retryPrompt, "outside the allowed list") || !strings.Contains(retryPrompt, "p-A") {
		t.Fatalf("retry prompt missing validation feedback or valid paragraph ID:\n%s", retryPrompt)
	}
	if strings.Contains(retryPrompt, "p-NONEXISTENT") {
		t.Fatalf("retry prompt should not repeat invalid paragraph IDs:\n%s", retryPrompt)
	}
}

func TestExtractSceneCardPromptRedactsParagraphIDsFromParagraphText(t *testing.T) {
	const validID = "p-01AAAAAAAAAAAAAAAAAAAAAAAA"
	const embeddedID = "p-01BBBBBBBBBBBBBBBBBBBBBBBB"
	paragraphs := []store.ParagraphRow{
		{ID: validID, ChapterID: "ch-0001", Ordinal: 1, Text: "A margin note mentions " + embeddedID + " as an old reference."},
	}
	scene := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: validID,
		ParagraphEnd:   validID,
	}
	fake := &fakeProvider{response: `{"title":"T","summary":"S.","evidence":["` + validID + `"]}`}

	_, err := compiler.ExtractSceneCardForTest(fake, scene, paragraphs, "test-model")
	if err != nil {
		t.Fatalf("extract scene card: %v", err)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("Generate calls = %d, want 1", len(fake.requests))
	}
	prompt := fake.requests[0].Messages[1].Content
	if strings.Contains(prompt, embeddedID) {
		t.Fatalf("prompt should redact paragraph-ID-looking text that is not collected context:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Allowed evidence paragraph IDs:\n- "+validID+"\n") {
		t.Fatalf("prompt missing allowed ID manifest:\n%s", prompt)
	}
	if !strings.Contains(prompt, "--- "+validID+" ---") {
		t.Fatalf("prompt missing paragraph header for valid ID:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[paragraph-id-redacted]") {
		t.Fatalf("prompt missing paragraph ID redaction marker:\n%s", prompt)
	}
}

func TestExtractSceneCardInvalidEvidenceFallsBack(t *testing.T) {
	paragraphs := []store.ParagraphRow{
		{ID: "p-A", ChapterID: "ch-0001", Ordinal: 1, Text: "She found the letter."},
	}
	scene := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-A",
		ParagraphEnd:   "p-A",
	}
	fake := &fakeProvider{response: `{"title":"T","summary":"S.","evidence":["p-NONEXISTENT"]}`}

	card, err := compiler.ExtractSceneCardForTest(fake, scene, paragraphs, "test-model")
	if err != nil {
		t.Fatalf("expected fallback after invalid retry, got %v", err)
	}
	if card.Summary != "She found the letter." {
		t.Fatalf("Summary = %q, want scene-text fallback", card.Summary)
	}
	if len(card.Evidence) != 1 || card.Evidence[0] != "p-A" {
		t.Fatalf("Evidence = %v, want [p-A]", card.Evidence)
	}
	if card.Recovery == nil || card.Recovery.Action != "fallback" || card.Recovery.Attempts != 2 {
		t.Fatalf("Recovery = %#v, want fallback after 2 attempts", card.Recovery)
	}
	if !strings.Contains(card.Recovery.Reason, "p-NONEXISTENT") {
		t.Fatalf("Recovery reason missing invalid paragraph ID: %q", card.Recovery.Reason)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("Generate calls = %d, want 2", len(fake.requests))
	}
}

func TestExtractSceneCardTimeoutRetriesWithCompactPrompt(t *testing.T) {
	paragraphs := timeoutRetryParagraphs()
	scene := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: paragraphs[0].ID,
		ParagraphEnd:   paragraphs[len(paragraphs)-1].ID,
	}
	fake := &fakeProvider{
		responses: []string{
			"",
			`{"title":"Compact","summary":"Opening survives compact retry.","evidence":["p-00"]}`,
		},
		errors: []error{context.DeadlineExceeded, nil},
	}

	card, err := compiler.ExtractSceneCardForTest(fake, scene, paragraphs, "test-model")
	if err != nil {
		t.Fatalf("expected compact retry to recover timeout, got %v", err)
	}
	if card.Title != "Compact" {
		t.Fatalf("Title = %q, want Compact", card.Title)
	}
	if len(card.Evidence) != 1 || card.Evidence[0] != "p-00" {
		t.Fatalf("Evidence = %v, want [p-00]", card.Evidence)
	}
	if card.Recovery == nil || card.Recovery.Action != "compact-retry" || card.Recovery.Attempts != 2 {
		t.Fatalf("Recovery = %#v, want compact-retry after 2 attempts", card.Recovery)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("Generate calls = %d, want 2", len(fake.requests))
	}
	compactPrompt := fake.requests[1].Messages[1].Content
	if !strings.Contains(compactPrompt, "compact evidence packet") {
		t.Fatalf("compact retry prompt missing timeout recovery framing:\n%s", compactPrompt)
	}
	if strings.Contains(compactPrompt, "OMITTED_MIDDLE") {
		t.Fatalf("compact retry prompt should omit middle-only paragraph text:\n%s", compactPrompt)
	}
	if strings.Contains(compactPrompt, "context deadline exceeded") {
		t.Fatalf("compact retry prompt should not include the raw timeout error:\n%s", compactPrompt)
	}
}

func TestExtractSceneCardTimeoutFallsBackAfterCompactRetryTimeout(t *testing.T) {
	paragraphs := timeoutRetryParagraphs()
	scene := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: paragraphs[0].ID,
		ParagraphEnd:   paragraphs[len(paragraphs)-1].ID,
	}
	fake := &fakeProvider{errors: []error{context.DeadlineExceeded, context.DeadlineExceeded}}

	card, err := compiler.ExtractSceneCardForTest(fake, scene, paragraphs, "test-model")
	if err != nil {
		t.Fatalf("expected fallback after repeated timeouts, got %v", err)
	}
	if card.Summary != "Opening paragraph for timeout retry." {
		t.Fatalf("Summary = %q, want scene-text fallback", card.Summary)
	}
	if len(card.Evidence) != 1 || card.Evidence[0] != "p-00" {
		t.Fatalf("Evidence = %v, want [p-00]", card.Evidence)
	}
	if card.Recovery == nil || card.Recovery.Action != "fallback" || card.Recovery.Attempts != 2 {
		t.Fatalf("Recovery = %#v, want fallback after 2 attempts", card.Recovery)
	}
	if !strings.Contains(card.Recovery.Reason, "context deadline exceeded") {
		t.Fatalf("Recovery reason missing timeout: %q", card.Recovery.Reason)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("Generate calls = %d, want 2", len(fake.requests))
	}
}

func timeoutRetryParagraphs() []store.ParagraphRow {
	paragraphs := make([]store.ParagraphRow, 15)
	for i := range paragraphs {
		paragraphs[i] = store.ParagraphRow{
			ID:        fmt.Sprintf("p-%02d", i),
			ChapterID: "ch-0001",
			Ordinal:   i + 1,
			Text:      fmt.Sprintf("Paragraph %02d text.", i),
		}
	}
	paragraphs[0].Text = "Opening paragraph for timeout retry. It establishes the scene."
	paragraphs[9].Text = "OMITTED_MIDDLE This text should not appear in the compact retry prompt."
	paragraphs[14].Text = "Closing paragraph for timeout retry."
	return paragraphs
}

func TestExtractSceneCardStrictInvalidEvidenceFails(t *testing.T) {
	paragraphs := []store.ParagraphRow{
		{ID: "p-A", ChapterID: "ch-0001", Ordinal: 1, Text: "She found the letter."},
	}
	scene := store.SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-A",
		ParagraphEnd:   "p-A",
	}
	fake := &fakeProvider{response: `{"title":"T","summary":"S.","evidence":["p-NONEXISTENT"]}`}

	_, err := compiler.ExtractSceneCardStrictForTest(fake, scene, paragraphs, "test-model")
	if err == nil {
		t.Fatal("expected strict extraction to fail for unknown evidence paragraph ID")
	}
	if len(fake.requests) != 1 {
		t.Fatalf("Generate calls = %d, want 1 in strict mode", len(fake.requests))
	}
}
