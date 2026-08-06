package query_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/query"
	"github.com/nusapuksic/story/internal/store"
)

// fakeProvider returns a fixed response for every Generate call.
type fakeProvider struct {
	response      string
	responses     []string
	err           error
	gotNilContext bool
	requests      []provider.GenerationRequest
}

func (f *fakeProvider) Health(_ context.Context) error { return f.err }
func (f *fakeProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{ID: "fake"}}, f.err
}
func (f *fakeProvider) Capabilities(_ context.Context, _ string) (provider.Capabilities, error) {
	return provider.Capabilities{Chat: true, JSONMode: true}, f.err
}
func (f *fakeProvider) Generate(ctx context.Context, req provider.GenerationRequest) (provider.GenerationResponse, error) {
	f.gotNilContext = ctx == nil
	idx := len(f.requests)
	f.requests = append(f.requests, req)
	content := f.response
	if idx < len(f.responses) {
		content = f.responses[idx]
	}
	return provider.GenerationResponse{Content: content}, f.err
}
func (f *fakeProvider) Embed(_ context.Context, _ provider.EmbeddingRequest) (provider.EmbeddingResponse, error) {
	return provider.EmbeddingResponse{}, f.err
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func seedStore(t *testing.T, st *store.Store) (paragraphID string) {
	t.Helper()
	if err := st.InsertChapterForTest("ch-0001", 1, "The Road"); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	const pid = "p-TESTID0001"
	if err := st.InsertParagraphWithTextForTest(pid, "ch-0001", 1,
		"Mara placed the unopened letter beneath the stove."); err != nil {
		t.Fatalf("insert paragraph: %v", err)
	}
	return pid
}

func TestAskReturnsAnswer(t *testing.T) {
	st := openTestStore(t)
	pid := seedStore(t, st)

	answerJSON := `{"answer":"Mara hides the letter.","evidence":["` + pid + `"],"uncertainties":[]}`
	fake := &fakeProvider{response: answerJSON}

	ans, err := query.Ask(context.Background(), st, fake, "fake-model",
		"Where does Mara put the letter?", query.Options{})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if ans.Answer == "" {
		t.Error("expected non-empty answer")
	}
	if len(ans.Evidence) != 1 {
		t.Errorf("expected 1 evidence item, got %d", len(ans.Evidence))
	}
	if ans.Evidence[0].ParagraphID != pid {
		t.Errorf("expected evidence paragraph %s, got %s", pid, ans.Evidence[0].ParagraphID)
	}
}

func TestAskNilContextUsesBackground(t *testing.T) {
	st := openTestStore(t)
	_ = seedStore(t, st)

	fake := &fakeProvider{response: `{"answer":"She hides it.","evidence":[],"uncertainties":[]}`}
	_, err := query.Ask(nil, st, fake, "fake-model", "Where does Mara put the letter?", query.Options{})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if fake.gotNilContext {
		t.Fatal("provider received nil context")
	}
}

func TestAskStripsInvalidCitations(t *testing.T) {
	st := openTestStore(t)
	_ = seedStore(t, st)

	// Model cites a paragraph ID that was NOT in the evidence packet.
	answerJSON := `{"answer":"She hides it.","evidence":["p-INVENTED-ID"],"uncertainties":[]}`
	fake := &fakeProvider{response: answerJSON}

	ans, err := query.Ask(context.Background(), st, fake, "fake-model",
		"Where does Mara put the letter?", query.Options{})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	// The invalid citation should be stripped.
	if len(ans.Evidence) != 0 {
		t.Errorf("expected 0 validated evidence items (invalid citation stripped), got %d", len(ans.Evidence))
	}
}

func TestAskInsufficientEvidence(t *testing.T) {
	st := openTestStore(t) // empty store – no paragraphs

	fake := &fakeProvider{response: `{"answer":"unknown"}`}
	_, err := query.Ask(context.Background(), st, fake, "fake-model",
		"What happens in the story?", query.Options{})
	if err == nil {
		t.Fatal("expected ErrInsufficientEvidence, got nil")
	}
	if !isInsufficientEvidence(err) {
		t.Errorf("expected ErrInsufficientEvidence, got: %v", err)
	}
}

func TestAskAllowsSummaryOnlyContext(t *testing.T) {
	st := openTestStore(t)
	fake := &fakeProvider{response: `{"answer":"The story is about memory and place.","evidence":[],"uncertainties":[]}`}

	ans, err := query.Ask(context.Background(), st, fake, "fake-model",
		"What is the theme of the story?", query.Options{
			Summaries: []query.SummaryContext{
				{
					RecordType: "book_summary",
					Summary:    "The book keeps returning to what places remember.",
					Themes:     []string{"The relationship between memory and physical environment"},
					Evidence:   []string{"ch-0001"},
				},
			},
		})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if ans.Answer == "" {
		t.Fatal("expected answer from summary context")
	}
	if len(fake.requests) != 1 {
		t.Fatalf("expected one model request, got %d", len(fake.requests))
	}
	prompt := fake.requests[0].Messages[1].Content
	if !strings.Contains(prompt, "## Summary context") {
		t.Fatalf("prompt missing summary context: %s", prompt)
	}
	if !strings.Contains(prompt, "The relationship between memory and physical environment") {
		t.Fatalf("prompt missing summary themes: %s", prompt)
	}
	if strings.Contains(prompt, "Supporting references") || strings.Contains(prompt, "- ch-0001") {
		t.Fatalf("prompt included non-citable summary supporting references: %s", prompt)
	}
}

func TestAskSummaryQuestionPrefersBookSummaryRecord(t *testing.T) {
	st := openTestStore(t)
	fake := &fakeProvider{response: `{"answer":"This is the book summary.","evidence":[],"records_used":["book_summary"],"uncertainties":[]}`}

	ans, err := query.Ask(context.Background(), st, fake, "fake-model", "Summarize the story.", query.Options{
		Summaries: []query.SummaryContext{
			{RecordType: "book_summary", Summary: "A book-level synopsis covers the whole story.", Themes: []string{"Memory"}},
			{RecordType: "chapter_summary", ChapterID: "ch-0001", ChapterTitle: "Opening", Summary: "The opening chapter summary."},
		},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if len(ans.RecordsUsed) != 1 || ans.RecordsUsed[0] != "book_summary" {
		t.Fatalf("RecordsUsed = %#v, want book_summary", ans.RecordsUsed)
	}
	prompt := fake.requests[0].Messages[1].Content
	if !strings.Contains(prompt, "[book_summary] Book summary") || !strings.Contains(prompt, "A book-level synopsis") {
		t.Fatalf("prompt missing book summary evidence:\n%s", prompt)
	}
	if strings.Contains(prompt, "The opening chapter summary") {
		t.Fatalf("story summary prompt should prefer book summary over chapter detail:\n%s", prompt)
	}
}

func TestAskCharacterInventoryUsesRolesAndEntities(t *testing.T) {
	st := openTestStore(t)
	_ = seedEntityContextForAsk(t, st)
	fake := &fakeProvider{response: `{"answer":"Mara is the protagonist.","evidence":[],"records_used":["character_role:char-mara","entity-mara"],"uncertainties":[]}`}

	ans, err := query.Ask(context.Background(), st, fake, "fake-model", "Who are the main characters in the story?", query.Options{
		CharacterRoles: []query.CharacterRoleContext{
			{
				CharacterID:     "char-mara",
				SourceEntityIDs: []string{"entity-mara"},
				CanonicalName:   "Mara Vale",
				Aliases:         []string{"Mara"},
				Classification:  "principal",
				Role:            "protagonist",
				Confidence:      0.94,
				Rationale:       "Mara drives the investigation.",
				Evidence:        []query.CharacterRoleEvidence{{SceneID: "sc-entity-context", Reason: "Shows Mara carrying the central action."}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if len(ans.RecordsUsed) != 2 || ans.RecordsUsed[0] != "character_role:char-mara" || ans.RecordsUsed[1] != "entity-mara" {
		t.Fatalf("RecordsUsed = %#v, want role and entity", ans.RecordsUsed)
	}
	prompt := fake.requests[0].Messages[1].Content
	for _, want := range []string{"## Character role context", "[character_role:char-mara]", "principal; protagonist", "Mara drives the investigation", "## Entity context", "[entity-mara]"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("character inventory prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestAskCharacterArcUsesNamedRoleWithoutCharacterKeyword(t *testing.T) {
	st := openTestStore(t)
	_ = seedEntityContextForAsk(t, st)
	fake := &fakeProvider{response: "{\"answer\":\"Mara grows less secretive.\",\"evidence\":[],\"records_used\":[\"character_role:char-mara\"],\"uncertainties\":[]}"}

	ans, err := query.Ask(context.Background(), st, fake, "fake-model", "How does Mara change?", query.Options{
		CharacterRoles: []query.CharacterRoleContext{
			{
				CharacterID:     "char-mara",
				SourceEntityIDs: []string{"entity-mara"},
				CanonicalName:   "Mara Vale",
				Aliases:         []string{"Mara"},
				Classification:  "principal",
				Role:            "protagonist",
				Rationale:       "Mara carries the central change arc.",
			},
		},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if len(ans.RecordsUsed) != 1 || ans.RecordsUsed[0] != "character_role:char-mara" {
		t.Fatalf("RecordsUsed = %#v, want character_role:char-mara", ans.RecordsUsed)
	}
	prompt := fake.requests[0].Messages[1].Content
	for _, want := range []string{"## Character role context", "[character_role:char-mara]", "Mara carries the central change arc"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("character arc prompt missing %q:\n%s", want, prompt)
		}
	}
}
func TestAskBroadOverflowCondensesAndBalancesParagraphs(t *testing.T) {
	st := openTestStore(t)
	for i := 1; i <= 6; i++ {
		chapterID := fmt.Sprintf("ch-%04d", i)
		pid := fmt.Sprintf("p-OVERVIEW%04d", i)
		if err := st.InsertChapterForTest(chapterID, i, fmt.Sprintf("Chapter %d", i)); err != nil {
			t.Fatalf("insert chapter %d: %v", i, err)
		}
		if err := st.InsertParagraphWithTextForTest(pid, chapterID, 1, fmt.Sprintf("Whole story marker %d.", i)); err != nil {
			t.Fatalf("insert paragraph %d: %v", i, err)
		}
	}
	fake := &fakeProvider{responses: []string{
		`{"summary":"The story moves from marker one to marker six.","support":["p-OVERVIEW0001","p-OVERVIEW0006"],"uncertainties":[]}`,
		`{"answer":"The story spans the first and final markers.","evidence":[],"records_used":["digest-0001"],"uncertainties":[]}`,
	}}

	ans, err := query.Ask(context.Background(), st, fake, "fake-model", "Give me a broad overview of the whole story.", query.Options{MaxEvidence: 2})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("Generate calls = %d, want condense + final answer", len(fake.requests))
	}
	if len(ans.RecordsUsed) != 1 || ans.RecordsUsed[0] != "digest-0001" {
		t.Fatalf("RecordsUsed = %#v, want digest-0001", ans.RecordsUsed)
	}
	prompt := fake.requests[1].Messages[1].Content
	for _, want := range []string{"## Condensed evidence", "[digest-0001]", "p-OVERVIEW0001", "p-OVERVIEW0006"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("broad overflow prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "p-OVERVIEW0002") {
		t.Fatalf("broad overflow prompt used opening-only truncation instead of balanced evidence:\n%s", prompt)
	}
}

func TestAskPrioritizesSummaryEvidenceParagraphs(t *testing.T) {
	st := openTestStore(t)
	if err := st.InsertChapterForTest("ch-0001", 1, "The Road"); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	const letterPID = "p-TESTID0001"
	if err := st.InsertParagraphWithTextForTest(letterPID, "ch-0001", 1,
		"Mara placed the unopened letter beneath the stove."); err != nil {
		t.Fatalf("insert letter paragraph: %v", err)
	}
	const themePID = "p-TESTID0002"
	if err := st.InsertParagraphWithTextForTest(themePID, "ch-0001", 2,
		"The exposed lakebed forces Mara to confront what the village tried to forget."); err != nil {
		t.Fatalf("insert theme paragraph: %v", err)
	}

	fake := &fakeProvider{response: `{"answer":"The theme is memory embedded in place.","evidence":["` + themePID + `"],"uncertainties":[]}`}
	ans, err := query.Ask(context.Background(), st, fake, "fake-model",
		"Where does Mara put the letter?", query.Options{
			MaxEvidence: 1,
			Summaries: []query.SummaryContext{
				{RecordType: "book_summary", Summary: "The story centers memory and place.", Themes: []string{"Memory embedded in place"}, Evidence: []string{"ch-0001"}},
				{RecordType: "chapter_summary", ChapterID: "ch-0001", Evidence: []string{themePID}},
			},
		})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if len(ans.Evidence) != 1 || ans.Evidence[0].ParagraphID != themePID {
		t.Fatalf("expected summary evidence %s to validate, got %#v", themePID, ans.Evidence)
	}
	prompt := fake.requests[0].Messages[1].Content
	if !strings.Contains(prompt, "Memory embedded in place") || !strings.Contains(prompt, themePID) {
		t.Fatalf("prompt missing prioritized summary context/evidence: %s", prompt)
	}
	if strings.Contains(prompt, letterPID) {
		t.Fatalf("prompt included lower-priority FTS paragraph despite MaxEvidence=1: %s", prompt)
	}
}

func TestAskMalformedModelResponse(t *testing.T) {
	st := openTestStore(t)
	_ = seedStore(t, st)

	fake := &fakeProvider{response: "not valid json at all"}
	_, err := query.Ask(context.Background(), st, fake, "fake-model",
		"Where does Mara put the letter?", query.Options{})
	if err == nil {
		t.Fatal("expected error for malformed model response")
	}
}

func TestAskEmptyModelResponseHasClearError(t *testing.T) {
	st := openTestStore(t)
	_ = seedStore(t, st)

	fake := &fakeProvider{response: "   "}
	_, err := query.Ask(context.Background(), st, fake, "fake-model",
		"Where does Mara put the letter?", query.Options{})
	if err == nil {
		t.Fatal("expected error for empty model response")
	}
	if !strings.Contains(err.Error(), "model returned empty response") {
		t.Fatalf("error = %q, want clear empty-response message", err.Error())
	}
}

func TestAskUsesProvidedQueryRunID(t *testing.T) {
	st := openTestStore(t)
	_ = seedStore(t, st)

	fake := &fakeProvider{response: `{"answer":"She hides it.","evidence":[],"uncertainties":[]}`}
	ans, err := query.Ask(context.Background(), st, fake, "fake-model",
		"Where does Mara put the letter?", query.Options{QueryRunID: "query-test-run"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if ans.QueryRunID != "query-test-run" {
		t.Fatalf("QueryRunID = %q, want query-test-run", ans.QueryRunID)
	}
}

func TestAskIncludesEntityContextForCharacterQuestion(t *testing.T) {
	st := openTestStore(t)
	pid := seedEntityContextForAsk(t, st)

	fake := &fakeProvider{response: `{"answer":"Mara knows the bell matters.","evidence":["` + pid + `"],"uncertainties":[]}`}
	_, err := query.Ask(context.Background(), st, fake, "fake-model", "What does Mara know about the bell?", query.Options{})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	prompt := fake.requests[0].Messages[1].Content
	for _, want := range []string{"## Entity context", "Mara (character)", "Aliases: Maraa", "sc-entity-context", "Mara; Maraa", "entities; participants"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("entity context prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestAskOmitsEntityContextForUnrelatedQuestion(t *testing.T) {
	st := openTestStore(t)
	_ = seedEntityContextForAsk(t, st)

	fake := &fakeProvider{response: `{"answer":"The letter is hidden.","evidence":[],"uncertainties":[]}`}
	_, err := query.Ask(context.Background(), st, fake, "fake-model", "Where is the letter hidden?", query.Options{})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	prompt := fake.requests[0].Messages[1].Content
	if strings.Contains(prompt, "## Entity context") {
		t.Fatalf("prompt included entity context for unrelated question:\n%s", prompt)
	}
}

func seedEntityContextForAsk(t *testing.T, st *store.Store) string {
	t.Helper()
	if err := st.InsertChapterForTest("ch-0001", 1, "The Road"); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	const pid = "p-ENTITYCTX0001"
	if err := st.InsertParagraphWithTextForTest(pid, "ch-0001", 1, "Mara keeps the bell secret beside the stove."); err != nil {
		t.Fatalf("insert paragraph: %v", err)
	}
	if err := st.InsertScene(store.SceneRow{
		ID:             "sc-entity-context",
		ChapterID:      "ch-0001",
		ParagraphStart: pid,
		ParagraphEnd:   pid,
		Ordinal:        1,
		BoundarySource: "explicit",
		Status:         "generated",
	}); err != nil {
		t.Fatalf("insert scene: %v", err)
	}
	if err := st.InsertEntity(store.EntityRow{
		ID:              "entity-mara",
		ChapterID:       "ch-0001",
		Type:            "character",
		CanonicalName:   "Mara",
		Aliases:         []string{"Maraa"},
		Evidence:        []string{"sc-entity-context"},
		GenerationRun:   "compile-test",
		GenerationModel: "test-model",
		PromptVersion:   "entity-resolution-v1",
		Status:          "generated",
		RawJSON:         "{}",
	}); err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	if err := st.InsertOccurrence(store.OccurrenceRow{
		EntityID:        "entity-mara",
		ChapterID:       "ch-0001",
		SceneID:         "sc-entity-context",
		SurfaceTexts:    []string{"Mara", "Maraa"},
		SourceFields:    []string{"entities", "participants"},
		Confidence:      0.95,
		GenerationRun:   "compile-test",
		GenerationModel: "test-model",
		PromptVersion:   "entity-resolution-v1",
		Status:          "generated",
		RawJSON:         "{}",
	}); err != nil {
		t.Fatalf("insert occurrence: %v", err)
	}
	return pid
}
func TestAskDefaultMode(t *testing.T) {
	st := openTestStore(t)
	_ = seedStore(t, st)

	fake := &fakeProvider{response: `{"answer":"She places it under the stove.","evidence":[]}`}
	ans, err := query.Ask(context.Background(), st, fake, "fake-model",
		"Where does Mara put the letter?", query.Options{})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if ans.Mode != "recall" {
		t.Errorf("expected default mode 'recall', got %s", ans.Mode)
	}
}

func TestAskWithMarkdownCodeFence(t *testing.T) {
	st := openTestStore(t)
	pid := seedStore(t, st)

	fencedJSON := "```json\n{\"answer\":\"She hides it.\",\"evidence\":[\"" + pid + "\"],\"uncertainties\":[]}\n```"
	fake := &fakeProvider{response: fencedJSON}

	ans, err := query.Ask(context.Background(), st, fake, "fake-model",
		"Where does Mara put the letter?", query.Options{})
	if err != nil {
		t.Fatalf("Ask with code fence: %v", err)
	}
	if ans.Answer == "" {
		t.Error("expected non-empty answer after stripping code fence")
	}
}

func seedSceneCardForAsk(t *testing.T, st *store.Store, status string) string {
	t.Helper()
	if err := st.InsertChapterForTest("ch-0001", 1, "The Road"); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	const pid = "p-SCENECARD0001"
	if err := st.InsertParagraphWithTextForTest(pid, "ch-0001", 1,
		"Mara folds the letter and waits by the cold stove."); err != nil {
		t.Fatalf("insert paragraph: %v", err)
	}
	if err := st.InsertScene(store.SceneRow{
		ID:             "sc-card-policy",
		ChapterID:      "ch-0001",
		ParagraphStart: pid,
		ParagraphEnd:   pid,
		Ordinal:        1,
		BoundarySource: "explicit",
		Status:         "generated",
	}); err != nil {
		t.Fatalf("insert scene: %v", err)
	}
	if err := st.InsertSceneCard(store.SceneCardRow{
		SceneID:         "sc-card-policy",
		Title:           "Generated secret context",
		Summary:         "Generated context says the letter changes hands.",
		Evidence:        []string{pid},
		GenerationRun:   "compile-test",
		GenerationModel: "test-model",
		PromptVersion:   "scene-extraction-v1",
		Status:          status,
		RawJSON:         "{}",
	}); err != nil {
		t.Fatalf("insert scene card: %v", err)
	}
	return pid
}

func TestAskExcludesGeneratedSceneCardsByDefault(t *testing.T) {
	st := openTestStore(t)
	seedSceneCardForAsk(t, st, "generated")
	fake := &fakeProvider{response: `{"answer":"Only manuscript text is available.","evidence":[],"uncertainties":[]}`}

	_, err := query.Ask(context.Background(), st, fake, "fake-model", "letter changes hands", query.Options{})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	prompt := fake.requests[0].Messages[1].Content
	if strings.Contains(prompt, "Generated secret context") || strings.Contains(prompt, "Generated context says") || strings.Contains(prompt, "## Scene context") {
		t.Fatalf("prompt included generated scene-card context by default: %s", prompt)
	}
}

func TestAskIncludeGeneratedAllowsGeneratedSceneCards(t *testing.T) {
	st := openTestStore(t)
	seedSceneCardForAsk(t, st, "generated")
	fake := &fakeProvider{response: `{"answer":"The generated card is available.","evidence":[],"uncertainties":[]}`}

	_, err := query.Ask(context.Background(), st, fake, "fake-model", "letter changes hands", query.Options{IncludeGenerated: true})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	prompt := fake.requests[0].Messages[1].Content
	if !strings.Contains(prompt, "Generated secret context") || !strings.Contains(prompt, "## Scene context") {
		t.Fatalf("prompt missing generated scene-card context with IncludeGenerated: %s", prompt)
	}
}
func TestAskEndingFallbackUsesTailSceneCardsAndEvidence(t *testing.T) {
	st := openTestStore(t)
	if err := st.InsertChapterForTest("ch-0001", 1, "The Road"); err != nil {
		t.Fatalf("insert chapter: %v", err)
	}
	var finalPID string
	for i := 1; i <= 8; i++ {
		pid := fmt.Sprintf("p-ENDING%04d", i)
		sceneID := fmt.Sprintf("sc-ending-%04d", i)
		if i == 8 {
			finalPID = pid
		}
		if err := st.InsertParagraphWithTextForTest(pid, "ch-0001", i, fmt.Sprintf("Marker paragraph %d.", i)); err != nil {
			t.Fatalf("insert paragraph %d: %v", i, err)
		}
		if err := st.InsertScene(store.SceneRow{
			ID:             sceneID,
			ChapterID:      "ch-0001",
			ParagraphStart: pid,
			ParagraphEnd:   pid,
			Ordinal:        i,
			BoundarySource: "explicit",
			Status:         "generated",
		}); err != nil {
			t.Fatalf("insert scene %d: %v", i, err)
		}
		if err := st.InsertSceneCard(store.SceneCardRow{
			SceneID:       sceneID,
			Title:         fmt.Sprintf("Scene card %d", i),
			Summary:       fmt.Sprintf("Structural summary %d.", i),
			Evidence:      []string{pid},
			PromptVersion: "scene-extraction-v1",
			Status:        "verified",
			RawJSON:       "{}",
		}); err != nil {
			t.Fatalf("insert scene card %d: %v", i, err)
		}
	}

	fake := &fakeProvider{response: `{"answer":"The story closes on the final marker.","evidence":["` + finalPID + `"],"uncertainties":[]}`}
	ans, err := query.Ask(context.Background(), st, fake, "fake-model", "How does the story end? Is it complete?", query.Options{})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if len(ans.Evidence) != 1 || ans.Evidence[0].ParagraphID != finalPID {
		t.Fatalf("expected final evidence %s to validate, got %#v", finalPID, ans.Evidence)
	}
	prompt := fake.requests[0].Messages[1].Content
	for _, want := range []string{"Scene card 5", "Scene card 8", finalPID, "Marker paragraph 8."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("ending fallback prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{"Scene card 1", "Scene card 4", "Marker paragraph 1."} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("ending fallback prompt included opening context %q:\n%s", unwanted, prompt)
		}
	}
}
func TestAskChapterFallbackSceneCardsStayInChapter(t *testing.T) {
	st := openTestStore(t)
	for _, ch := range []struct {
		id      string
		ordinal int
		pid     string
		sceneID string
		title   string
		summary string
	}{
		{id: "ch-0001", ordinal: 1, pid: "p-ASKCHAPTER0001", sceneID: "sc-ask-chapter-1", title: "Chapter one fallback context", summary: "Only chapter one should appear."},
		{id: "ch-0002", ordinal: 2, pid: "p-ASKCHAPTER0002", sceneID: "sc-ask-chapter-2", title: "Chapter two fallback context", summary: "Chapter two must stay out."},
	} {
		if err := st.InsertChapterForTest(ch.id, ch.ordinal, ch.id); err != nil {
			t.Fatalf("insert chapter %s: %v", ch.id, err)
		}
		if err := st.InsertParagraphWithTextForTest(ch.pid, ch.id, 1, ch.summary); err != nil {
			t.Fatalf("insert paragraph %s: %v", ch.pid, err)
		}
		if err := st.InsertScene(store.SceneRow{
			ID:             ch.sceneID,
			ChapterID:      ch.id,
			ParagraphStart: ch.pid,
			ParagraphEnd:   ch.pid,
			Ordinal:        1,
			BoundarySource: "chapter_end",
			Status:         "generated",
		}); err != nil {
			t.Fatalf("insert scene %s: %v", ch.sceneID, err)
		}
		if err := st.InsertSceneCard(store.SceneCardRow{
			SceneID:         ch.sceneID,
			Title:           ch.title,
			Summary:         ch.summary,
			Evidence:        []string{ch.pid},
			GenerationRun:   "compile-test",
			GenerationModel: "test-model",
			PromptVersion:   "scene-extraction-v1",
			Status:          "verified",
			RawJSON:         "{}",
		}); err != nil {
			t.Fatalf("insert scene card %s: %v", ch.sceneID, err)
		}
	}
	fake := &fakeProvider{response: `{"answer":"Only chapter one context is available.","evidence":[],"uncertainties":[]}`}

	_, err := query.Ask(context.Background(), st, fake, "fake-model", "zzzxxy", query.Options{ChapterID: "ch-0001"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	prompt := fake.requests[0].Messages[1].Content
	if !strings.Contains(prompt, "Chapter one fallback context") || !strings.Contains(prompt, "## Scene context") {
		t.Fatalf("prompt missing chapter-scoped fallback scene card: %s", prompt)
	}
	if strings.Contains(prompt, "Chapter two fallback context") || strings.Contains(prompt, "ch-0002") {
		t.Fatalf("prompt included cross-chapter fallback context: %s", prompt)
	}
}
func isInsufficientEvidence(err error) bool {
	return err != nil && err.Error() == query.ErrInsufficientEvidence.Error()
}
