package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	storyprompts "github.com/nusapuksic/story/internal/prompts"
	"github.com/nusapuksic/story/internal/provider"
	"github.com/nusapuksic/story/internal/store"
)

const minimumCondenseParagraphChunk = 8

type rawEvidenceDigest struct {
	Summary       string   `json:"summary"`
	Support       []string `json:"support"`
	Uncertainties []string `json:"uncertainties"`
}

func condenseParagraphEvidence(
	ctx context.Context,
	prov provider.Provider,
	model string,
	question string,
	opts Options,
	paragraphs []store.ParagraphRow,
) ([]EvidenceDigest, error) {
	if len(paragraphs) == 0 || prov == nil {
		return nil, nil
	}
	chunkSize := opts.MaxEvidence * 2
	if chunkSize < minimumCondenseParagraphChunk {
		chunkSize = minimumCondenseParagraphChunk
	}

	loadedPrompt, err := storyprompts.Load(opts.PromptsDir, storyprompts.CondenseEvidence)
	if err != nil {
		fallback, _ := storyprompts.Default(storyprompts.CondenseEvidence)
		loadedPrompt = fallback
	}
	systemPrompt := strings.TrimSpace(loadedPrompt.Content)
	digests := make([]EvidenceDigest, 0, (len(paragraphs)+chunkSize-1)/chunkSize)
	for start := 0; start < len(paragraphs); start += chunkSize {
		end := start + chunkSize
		if end > len(paragraphs) {
			end = len(paragraphs)
		}
		chunk := paragraphs[start:end]
		req := provider.GenerationRequest{
			Model: model,
			Messages: []provider.Message{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: buildCondenseEvidencePrompt(question, opts.Mode, chunk)},
			},
			Temperature: 0.1,
			MaxTokens:   900,
			JSONMode:    true,
		}
		resp, err := prov.Generate(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("digest %d model call: %w", len(digests)+1, err)
		}
		raw, err := parseEvidenceDigestResponse(resp.Content)
		if err != nil {
			return nil, fmt.Errorf("digest %d response: %w", len(digests)+1, err)
		}
		support := validateDigestSupport(raw.Support, chunk)
		digest := EvidenceDigest{
			ID:            fmt.Sprintf("digest-%04d", len(digests)+1),
			Scope:         paragraphChunkScope(chunk),
			Summary:       strings.TrimSpace(raw.Summary),
			Support:       support,
			SourceRecords: support,
			Uncertainties: compactStrings(raw.Uncertainties),
		}
		if digest.Summary == "" {
			return nil, errors.New("model returned empty digest summary")
		}
		digests = append(digests, digest)
	}
	return digests, nil
}

func buildCondenseEvidencePrompt(question, mode string, paragraphs []store.ParagraphRow) string {
	var sb strings.Builder
	sb.WriteString("## Task\n\n")
	sb.WriteString("Condense these evidence paragraphs for a later answer to the question. Preserve coverage across the supplied range. Do not answer the question directly.\n\n")
	sb.WriteString("Mode: ")
	sb.WriteString(strings.TrimSpace(mode))
	sb.WriteString("\n\n")
	sb.WriteString("Question:\n")
	sb.WriteString(question)
	sb.WriteString("\n\n")
	sb.WriteString("## Evidence paragraphs\n\n")
	for _, p := range paragraphs {
		sb.WriteString("[")
		sb.WriteString(p.ID)
		sb.WriteString("] (")
		sb.WriteString(p.ChapterID)
		sb.WriteString(")\n")
		sb.WriteString(p.Text)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Return JSON only: {\"summary\":\"...\",\"support\":[\"p-...\"],\"uncertainties\":[\"...\"]}. Use only support IDs listed above.")
	return sb.String()
}

func parseEvidenceDigestResponse(content string) (rawEvidenceDigest, error) {
	content = stripMarkdownJSONFence(content)
	if strings.TrimSpace(content) == "" {
		return rawEvidenceDigest{}, errors.New("model returned empty response")
	}
	var digest rawEvidenceDigest
	if err := json.Unmarshal([]byte(content), &digest); err != nil {
		return rawEvidenceDigest{}, fmt.Errorf("unmarshal digest JSON: %w", err)
	}
	if strings.TrimSpace(digest.Summary) == "" {
		return rawEvidenceDigest{}, errors.New("model returned empty summary")
	}
	return digest, nil
}

func validateDigestSupport(ids []string, paragraphs []store.ParagraphRow) []string {
	valid := make(map[string]bool, len(paragraphs))
	for _, p := range paragraphs {
		valid[p.ID] = true
	}
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || !valid[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func paragraphChunkScope(paragraphs []store.ParagraphRow) string {
	if len(paragraphs) == 0 {
		return ""
	}
	first := paragraphs[0]
	last := paragraphs[len(paragraphs)-1]
	if first.ChapterID == last.ChapterID {
		return fmt.Sprintf("%s paragraphs %d-%d", first.ChapterID, first.Ordinal, last.Ordinal)
	}
	return fmt.Sprintf("%s paragraph %d through %s paragraph %d", first.ChapterID, first.Ordinal, last.ChapterID, last.Ordinal)
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
