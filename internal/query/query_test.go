package query_test

import (
	"context"
	"testing"

	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/query"
	"github.com/nusapuksic/story/internal/store"
)

// fakeProvider returns a fixed response for every Generate call.
type fakeProvider struct {
	response string
	err      error
}

func (f *fakeProvider) Health(_ context.Context) error { return f.err }
func (f *fakeProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{ID: "fake"}}, f.err
}
func (f *fakeProvider) Capabilities(_ context.Context, _ string) (provider.Capabilities, error) {
	return provider.Capabilities{Chat: true, JSONMode: true}, f.err
}
func (f *fakeProvider) Generate(_ context.Context, _ provider.GenerationRequest) (provider.GenerationResponse, error) {
	return provider.GenerationResponse{Content: f.response}, f.err
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

func isInsufficientEvidence(err error) bool {
	return err != nil && err.Error() == query.ErrInsufficientEvidence.Error()
}
