// Package query implements the evidence-backed story ask pipeline.
//
// The pipeline:
//  1. Retrieve relevant scene cards and paragraphs (FTS search).
//  2. Add generated summary context and supporting paragraph evidence.
//  3. Collect the paragraph text for matched scenes.
//  4. Construct a bounded evidence packet.
//  5. Call the discussion model.
//  6. Validate all evidence identifiers returned by the model.
//  7. Return the answer with provenance.
package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nusapuksic/story/internal/ids"
	storyprompts "github.com/nusapuksic/story/internal/prompts"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/retrieval"
	"github.com/nusapuksic/story/internal/store"
)

// ErrInsufficientEvidence is returned when the retrieval step cannot find
// enough evidence to attempt answering the question.
var ErrInsufficientEvidence = errors.New("insufficient evidence to answer the question")

// EvidenceItem is one piece of evidence cited in an answer.
type EvidenceItem struct {
	ParagraphID string `json:"paragraph_id"`
	ChapterID   string `json:"chapter_id"`
}

// SummaryContext is a generated chapter or book summary made available to ask.
type SummaryContext struct {
	RecordType    string
	ChapterID     string
	ChapterTitle  string
	Summary       string
	Themes        []string
	Unresolved    []string
	Evidence      []string
	SourceRecords []string
}

// Answer is the result of an Ask call.
type Answer struct {
	// Answer is the model's prose answer grounded in the evidence.
	Answer string `json:"answer"`
	// Mode is the query mode used (e.g. "recall", "continuity").
	Mode string `json:"mode"`
	// Evidence contains the paragraph citations validated against the evidence
	// packet.  Citations not present in the packet are removed.
	Evidence []EvidenceItem `json:"evidence"`
	// Uncertainties are hedges or open questions noted by the model.
	Uncertainties []string `json:"uncertainties,omitempty"`
	// QueryRunID is the identifier for this query run.
	QueryRunID    string `json:"model_run"`
	PromptVersion string `json:"prompt_version,omitempty"`
}

// Options controls a query.
type Options struct {
	// QueryRunID optionally supplies the identifier for this query run. Empty
	// generates a fresh query run ID.
	QueryRunID string
	// Mode is the query mode: recall, continuity, interpretation, style,
	// development.  Defaults to "recall".
	Mode string
	// ChapterID restricts evidence to a specific chapter.
	ChapterID string
	// IncludeGenerated opts into scene cards with status "generated".
	// By default only verified/accepted scene cards are used as context.
	// SceneCardStatusPolicy wins when set.
	IncludeGenerated bool
	// SceneCardStatusPolicy controls which scene-card statuses are available as
	// generated context. Empty derives from IncludeGenerated.
	SceneCardStatusPolicy store.SceneCardStatusPolicy
	// MaxEvidence is the maximum number of paragraphs to include in the
	// evidence packet (default 20).
	MaxEvidence int
	// PromptsDir is the project prompts directory. Empty uses embedded defaults.
	PromptsDir string
	// Summaries are generated book/chapter summaries to include as high-level
	// context, especially for interpretive questions.
	Summaries []SummaryContext
}

// rawAnswer is the LLM response structure before validation.
type rawAnswer struct {
	Answer        string   `json:"answer"`
	Evidence      []string `json:"evidence"`
	Uncertainties []string `json:"uncertainties"`
}

const (
	defaultFallbackSceneCardLimit = 6
	endingFallbackSceneCardLimit  = 4
)

// Ask runs the evidence-backed query pipeline against the indexed project.
// It calls the configured discussion model and returns a validated answer.
func Ask(
	ctx context.Context,
	st *store.Store,
	prov provider.Provider,
	model string,
	question string,
	opts Options,
) (*Answer, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Mode == "" {
		opts.Mode = "recall"
	}
	if opts.MaxEvidence <= 0 {
		opts.MaxEvidence = 20
	}
	cardPolicy := sceneCardStatusPolicy(opts)

	// Step 1: Retrieve relevant scene cards and paragraphs via FTS.
	ret, err := retrieval.Search(st, question, retrieval.Options{
		ChapterID:             opts.ChapterID,
		MaxParagraphs:         opts.MaxEvidence,
		MaxSceneCards:         10,
		SceneCardStatusPolicy: cardPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("retrieval: %w", err)
	}

	endingQuestion := isEndingQuestion(question)

	// Step 1b: FTS fallback. If keyword search found nothing, use a bounded
	// structural slice instead of sending every scene card. Ending questions
	// need the tail of the manuscript; broad questions get bookends.
	if len(ret.Paragraphs) == 0 && len(ret.SceneCards) == 0 {
		cards, err := st.AllSceneCardsByStatusPolicyForChapter(opts.ChapterID, cardPolicy)
		if err != nil {
			return nil, fmt.Errorf("fallback scene card retrieval: %w", err)
		}
		ret.SceneCards = selectFallbackSceneCards(question, cards)
	}

	paragraphs := make([]store.ParagraphRow, 0, len(ret.Paragraphs))
	paraByID := make(map[string]store.ParagraphRow, len(ret.Paragraphs))
	addParagraph := func(p store.ParagraphRow) {
		if strings.TrimSpace(p.ID) == "" {
			return
		}
		if _, ok := paraByID[p.ID]; ok {
			return
		}
		paraByID[p.ID] = p
		paragraphs = append(paragraphs, p)
	}

	addRetrievedParagraphs := func() {
		for _, p := range ret.Paragraphs {
			addParagraph(p)
		}
	}
	addSummaryEvidenceParagraphs := func() {
		for _, p := range summaryEvidenceParagraphs(st, opts.Summaries, opts.ChapterID) {
			addParagraph(p)
		}
	}
	addSceneCardEvidenceParagraphs := func() {
		for _, card := range ret.SceneCards {
			for _, pid := range card.Evidence {
				if _, ok := paraByID[pid]; ok {
					continue
				}
				p, err := st.InspectParagraph(pid)
				if err != nil {
					continue
				}
				addParagraph(p)
			}
		}
	}

	if endingQuestion {
		// Ending/completeness questions need citable final-scene paragraphs more
		// than broad summary support, especially when the evidence packet is capped.
		addRetrievedParagraphs()
		addSceneCardEvidenceParagraphs()
		addSummaryEvidenceParagraphs()
	} else {
		// Summary evidence is ranked first so high-level theme/context answers do
		// not lose their support when the evidence packet is capped.
		addSummaryEvidenceParagraphs()
		addRetrievedParagraphs()
		addSceneCardEvidenceParagraphs()
	}

	usedParagraphFallback := false

	// Step 3b: If still no paragraphs (e.g. no scene cards compiled yet),
	// gather all indexed paragraphs from all chapters as a broad fallback.
	// This ensures the question can still be answered from source text alone.
	if len(paragraphs) == 0 {
		usedParagraphFallback = true
		chapters, chErr := st.AllChapters()
		if chErr == nil {
			for _, ch := range chapters {
				if opts.ChapterID != "" && ch.ID != opts.ChapterID {
					continue
				}
				paras, pErr := st.ParagraphsByChapter(ch.ID)
				if pErr != nil {
					continue
				}
				for _, p := range paras {
					addParagraph(p)
				}
			}
		}
	}
	// Step 4: Check whether we have enough context.
	if len(paragraphs) == 0 && len(ret.SceneCards) == 0 && len(opts.Summaries) == 0 {
		return nil, ErrInsufficientEvidence
	}

	// Cap paragraphs at MaxEvidence. If an ending question fell back to raw
	// manuscript paragraphs, keep the tail rather than the opening.
	if len(paragraphs) > opts.MaxEvidence {
		if endingQuestion && usedParagraphFallback {
			paragraphs = paragraphs[len(paragraphs)-opts.MaxEvidence:]
		} else {
			paragraphs = paragraphs[:opts.MaxEvidence]
		}
	}

	// Build a set of valid paragraph IDs for citation validation.
	validIDs := make(map[string]string, len(paragraphs)) // id → chapter_id
	for _, p := range paragraphs {
		validIDs[p.ID] = p.ChapterID
	}

	entityContext, err := entityContextForQuestion(st, question, opts.Mode, opts.ChapterID)
	if err != nil {
		return nil, fmt.Errorf("entity context: %w", err)
	}

	// Step 5: Build the evidence packet and call the discussion model.
	loadedPrompt, err := storyprompts.Load(opts.PromptsDir, storyprompts.AnswerQuestion)
	if err != nil {
		fallback, _ := storyprompts.Default(storyprompts.AnswerQuestion)
		loadedPrompt = fallback
	}
	systemPrompt := buildSystemPrompt(loadedPrompt.Content, opts.Mode)
	userPrompt := buildUserPrompt(question, opts.Mode, opts.Summaries, entityContext, ret.SceneCards, paragraphs)

	queryRunID := strings.TrimSpace(opts.QueryRunID)
	if queryRunID == "" {
		queryRunID = ids.NewQueryRunID()
	}
	req := provider.GenerationRequest{
		Model: model,
		Messages: []provider.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.2,
		MaxTokens:   2000,
		JSONMode:    true,
	}

	resp, err := prov.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("discussion model call: %w", err)
	}

	// Step 6: Parse and validate the model response.
	raw, err := parseAnswerResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse model response: %w", err)
	}

	// Step 7: Validate evidence citations – remove any IDs not in the packet.
	var validatedEvidence []EvidenceItem
	for _, pid := range raw.Evidence {
		chapterID, ok := validIDs[pid]
		if !ok {
			continue // citation not in evidence packet; drop it
		}
		validatedEvidence = append(validatedEvidence, EvidenceItem{
			ParagraphID: pid,
			ChapterID:   chapterID,
		})
	}

	return &Answer{
		Answer:        strings.TrimSpace(raw.Answer),
		Mode:          opts.Mode,
		Evidence:      validatedEvidence,
		Uncertainties: raw.Uncertainties,
		QueryRunID:    queryRunID,
		PromptVersion: loadedPrompt.Version,
	}, nil
}

func selectFallbackSceneCards(question string, cards []store.SceneCardRow) []store.SceneCardRow {
	if len(cards) == 0 {
		return nil
	}
	if isEndingQuestion(question) {
		return tailSceneCards(cards, endingFallbackSceneCardLimit)
	}
	return bookendSceneCards(cards, defaultFallbackSceneCardLimit)
}

func tailSceneCards(cards []store.SceneCardRow, limit int) []store.SceneCardRow {
	if limit <= 0 || len(cards) <= limit {
		return cards
	}
	out := make([]store.SceneCardRow, limit)
	copy(out, cards[len(cards)-limit:])
	return out
}

func bookendSceneCards(cards []store.SceneCardRow, limit int) []store.SceneCardRow {
	if limit <= 0 || len(cards) <= limit {
		return cards
	}
	head := limit / 2
	tail := limit - head
	out := make([]store.SceneCardRow, 0, limit)
	out = append(out, cards[:head]...)
	out = append(out, cards[len(cards)-tail:]...)
	return out
}

func isEndingQuestion(question string) bool {
	normalized := normalizeQuestionText(question)
	for _, phrase := range []string{
		"how does the story end",
		"how does it end",
		"how does this end",
		"story end",
		"story ends",
		"book end",
		"book ends",
		"manuscript end",
		"manuscript ends",
		"novel end",
		"novel ends",
		"at the end of the story",
		"by the end of the story",
		"at the end of the book",
		"by the end of the book",
		"at the end of the novel",
		"by the end of the novel",
		"at the end of the manuscript",
		"by the end of the manuscript",
		"the ending",
		"ending",
		"conclusion",
		"concludes",
		"resolution",
		"resolved",
		"epilogue",
		"final scene",
		"final chapter",
		"last scene",
		"last chapter",
		"last paragraph",
		"last page",
		"is it complete",
		"does it feel complete",
		"feels complete",
		"complete ending",
	} {
		if containsQuestionPhrase(normalized, phrase) {
			return true
		}
	}
	return false
}

func normalizeQuestionText(question string) string {
	lower := strings.ToLower(question)
	normalized := strings.NewReplacer(
		"?", " ",
		".", " ",
		",", " ",
		"!", " ",
		";", " ",
		":", " ",
		"-", " ",
		"_", " ",
		"/", " ",
		"\\", " ",
		"'", " ",
		"\"", " ",
	).Replace(lower)
	return strings.Join(strings.Fields(normalized), " ")
}

func containsQuestionPhrase(normalizedQuestion, phrase string) bool {
	phrase = normalizeQuestionText(phrase)
	if phrase == "" {
		return false
	}
	return strings.Contains(" "+normalizedQuestion+" ", " "+phrase+" ")
}
func sceneCardStatusPolicy(opts Options) store.SceneCardStatusPolicy {
	if opts.SceneCardStatusPolicy != "" {
		return opts.SceneCardStatusPolicy
	}
	if opts.IncludeGenerated {
		return store.SceneCardStatusIncludeGenerated
	}
	return store.SceneCardStatusTrustedOnly
}

func summaryEvidenceParagraphs(st *store.Store, summaries []SummaryContext, chapterID string) []store.ParagraphRow {
	if len(summaries) == 0 {
		return nil
	}

	chapterEvidence := make(map[string][]string)
	for _, summary := range summaries {
		if summary.RecordType != "chapter_summary" || summary.ChapterID == "" {
			continue
		}
		for _, evidenceID := range summary.Evidence {
			if isParagraphReference(evidenceID) {
				chapterEvidence[summary.ChapterID] = append(chapterEvidence[summary.ChapterID], strings.TrimSpace(evidenceID))
			}
		}
	}

	var ids []string
	seen := make(map[string]bool)
	addID := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	for _, summary := range summaries {
		if chapterID != "" && summary.RecordType == "chapter_summary" && summary.ChapterID != chapterID {
			continue
		}
		for _, evidenceID := range summary.Evidence {
			switch {
			case isParagraphReference(evidenceID):
				addID(evidenceID)
			case isChapterReference(evidenceID):
				chapterRef := strings.TrimSpace(evidenceID)
				if chapterID != "" && chapterRef != chapterID {
					continue
				}
				for _, paragraphID := range chapterEvidence[chapterRef] {
					addID(paragraphID)
				}
			}
		}
	}

	paragraphs := make([]store.ParagraphRow, 0, len(ids))
	for _, id := range ids {
		p, err := st.InspectParagraph(id)
		if err != nil {
			continue
		}
		paragraphs = append(paragraphs, p)
	}
	return paragraphs
}

func isParagraphReference(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), "p-")
}

func isChapterReference(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), "ch-")
}

// parseAnswerResponse parses the LLM response JSON.  It tolerates markdown
// code fences around the JSON object.
func parseAnswerResponse(content string) (rawAnswer, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		if i := strings.Index(content, "\n"); i >= 0 {
			content = content[i+1:]
		}
		if i := strings.LastIndex(content, "```"); i >= 0 {
			content = content[:i]
		}
		content = strings.TrimSpace(content)
	}
	if content == "" {
		return rawAnswer{}, errors.New("model returned empty response")
	}

	var a rawAnswer
	if err := json.Unmarshal([]byte(content), &a); err != nil {
		return rawAnswer{}, fmt.Errorf("unmarshal answer JSON: %w", err)
	}
	if strings.TrimSpace(a.Answer) == "" {
		return rawAnswer{}, errors.New("model returned empty answer")
	}
	return a, nil
}
