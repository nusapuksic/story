package query

import (
	"strings"

	"github.com/nusapuksic/story/internal/store"
)

// buildSystemPrompt returns the system prompt for the discussion model.
func buildSystemPrompt(mode string) string {
	base := `You are a literary analyst answering questions about a fiction manuscript.
Answer strictly from the evidence provided. Do not use general narrative expectations.
Return ONLY a JSON object with this exact schema:
{"answer":"...","evidence":["p-...","p-..."],"uncertainties":["..."]}

Rules:
- "answer": your prose answer grounded in the evidence.
- "evidence": list only paragraph IDs from the provided evidence that directly support your answer. Omit IDs that do not support the answer.
- "uncertainties": list genuine gaps or unresolved questions from the manuscript. Omit if none.
- Cite no paragraph IDs that were not provided to you.
- If the evidence is insufficient, say so in "answer" and leave "evidence" empty.`

	switch mode {
	case "continuity":
		return base + "\nFocus on continuity: what characters know, believe, and have experienced at specific story moments."
	case "interpretation":
		return base + "\nFocus on interpretation: themes, motifs, symbolic meaning, and authorial intent."
	case "style":
		return base + "\nFocus on style: narrative voice, prose technique, structural choices, and language patterns."
	case "development":
		return base + "\nFocus on development: character arcs, relationship changes, and plot progression."
	default: // "recall"
		return base + "\nFocus on recall: factual events, stated facts, and explicit manuscript content."
	}
}

// buildUserPrompt constructs the user-turn message including scene card
// context, evidence paragraphs, and the question.
func buildUserPrompt(
	question, mode string,
	cards []store.SceneCardRow,
	paragraphs []store.ParagraphRow,
) string {
	var sb strings.Builder

	if len(cards) > 0 {
		sb.WriteString("## Scene context\n\n")
		for _, c := range cards {
			sb.WriteString("[")
			sb.WriteString(c.SceneID)
			sb.WriteString("] ")
			sb.WriteString(c.Title)
			sb.WriteString("\n")
			sb.WriteString(c.Summary)
			sb.WriteString("\n\n")
		}
	}

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

	sb.WriteString("## Question\n\n")
	sb.WriteString(question)
	sb.WriteString("\n\n")
	sb.WriteString("Answer in JSON as specified. Cite only paragraph IDs listed above.")

	return sb.String()
}
