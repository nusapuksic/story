// Package query implements the evidence-backed story ask pipeline.
//
// The pipeline:
//  1. Retrieve relevant scene cards and paragraphs (FTS search).
//  2. Collect the paragraph text for matched scenes.
//  3. Construct a bounded evidence packet.
//  4. Call the discussion model.
//  5. Validate all evidence identifiers returned by the model.
//  6. Return the answer with provenance.
package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nusapuksic/story/internal/ids"
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
	QueryRunID string `json:"model_run"`
}

// Options controls a query.
type Options struct {
	// Mode is the query mode: recall, continuity, interpretation, style,
	// development.  Defaults to "recall".
	Mode string
	// ChapterID restricts evidence to a specific chapter.
	ChapterID string
	// IncludeGenerated allows scene cards with status "generated" to be used
	// as evidence context.  By default only verified/accepted cards are used;
	// since v0.1 review is not yet implemented, this defaults to true.
	IncludeGenerated bool
	// MaxEvidence is the maximum number of paragraphs to include in the
	// evidence packet (default 20).
	MaxEvidence int
}

// rawAnswer is the LLM response structure before validation.
type rawAnswer struct {
	Answer        string   `json:"answer"`
	Evidence      []string `json:"evidence"`
	Uncertainties []string `json:"uncertainties"`
}

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
	if opts.Mode == "" {
		opts.Mode = "recall"
	}
	if opts.MaxEvidence <= 0 {
		opts.MaxEvidence = 20
	}

	// Step 1: Retrieve relevant scene cards and paragraphs via FTS.
	ret, err := retrieval.Search(st, question, retrieval.Options{
		ChapterID:     opts.ChapterID,
		MaxParagraphs: opts.MaxEvidence,
		MaxSceneCards: 10,
	})
	if err != nil {
		return nil, fmt.Errorf("retrieval: %w", err)
	}

	// Step 1b: FTS fallback – if the keyword search found nothing, gather all
	// scene cards so the model can still answer from structural context.
	if len(ret.Paragraphs) == 0 && len(ret.SceneCards) == 0 {
		cards, err := st.AllSceneCards()
		if err != nil {
			return nil, fmt.Errorf("fallback scene card retrieval: %w", err)
		}
		ret.SceneCards = cards
	}

	// Step 2: For each matched scene card, also pull in any paragraphs from
	// the scene that are not already in the retrieved set.
	paraBylID := make(map[string]store.ParagraphRow, len(ret.Paragraphs))
	for _, p := range ret.Paragraphs {
		paraBylID[p.ID] = p
	}
	for _, card := range ret.SceneCards {
		for _, pid := range card.Evidence {
			if _, ok := paraBylID[pid]; ok {
				continue
			}
			p, err := st.InspectParagraph(pid)
			if err != nil {
				continue
			}
			paraBylID[pid] = p
			ret.Paragraphs = append(ret.Paragraphs, p)
		}
	}

	// Step 2b: If still no paragraphs (e.g. no scene cards compiled yet),
	// gather all indexed paragraphs from all chapters as a broad fallback.
	// This ensures the question can still be answered from source text alone.
	if len(ret.Paragraphs) == 0 {
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
					if _, ok := paraBylID[p.ID]; !ok {
						paraBylID[p.ID] = p
						ret.Paragraphs = append(ret.Paragraphs, p)
					}
				}
			}
		}
	}

	// Step 3: Check whether we have enough evidence.
	if len(ret.Paragraphs) == 0 && len(ret.SceneCards) == 0 {
		return nil, ErrInsufficientEvidence
	}

	// Cap paragraphs at MaxEvidence.
	paragraphs := ret.Paragraphs
	if len(paragraphs) > opts.MaxEvidence {
		paragraphs = paragraphs[:opts.MaxEvidence]
	}

	// Build a set of valid paragraph IDs for citation validation.
	validIDs := make(map[string]string, len(paragraphs)) // id → chapter_id
	for _, p := range paragraphs {
		validIDs[p.ID] = p.ChapterID
	}

	// Step 4: Build the evidence packet and call the discussion model.
	systemPrompt := buildSystemPrompt(opts.Mode)
	userPrompt := buildUserPrompt(question, opts.Mode, ret.SceneCards, paragraphs)

	queryRunID := ids.NewQueryRunID()
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

	// Step 5: Parse and validate the model response.
	raw, err := parseAnswerResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse model response: %w", err)
	}

	// Step 6: Validate evidence citations – remove any IDs not in the packet.
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
	}, nil
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

	var a rawAnswer
	if err := json.Unmarshal([]byte(content), &a); err != nil {
		return rawAnswer{}, fmt.Errorf("unmarshal answer JSON: %w", err)
	}
	if strings.TrimSpace(a.Answer) == "" {
		return rawAnswer{}, errors.New("model returned empty answer")
	}
	return a, nil
}
