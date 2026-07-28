package query

import (
	"strings"

	"github.com/nusapuksic/story/internal/store"
)

// buildSystemPrompt returns the system prompt for the discussion model.
func buildSystemPrompt(base, mode string) string {
	base = strings.TrimSpace(base)
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
	summaries []SummaryContext,
	cards []store.SceneCardRow,
	paragraphs []store.ParagraphRow,
) string {
	var sb strings.Builder

	if hasVisibleSummaryContext(summaries) {
		sb.WriteString("## Summary context\n\n")
		sb.WriteString("Use this generated context for high-level interpretation. It is not a citation source; cite only paragraph IDs from the evidence paragraphs below.\n\n")
		for _, s := range summaries {
			if !isVisibleSummaryContext(s) {
				continue
			}
			writeSummaryContext(&sb, s)
		}
	}

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

func hasVisibleSummaryContext(summaries []SummaryContext) bool {
	for _, s := range summaries {
		if isVisibleSummaryContext(s) {
			return true
		}
	}
	return false
}

func isVisibleSummaryContext(s SummaryContext) bool {
	return strings.TrimSpace(s.Summary) != "" || hasListValue(s.Themes) || hasListValue(s.Unresolved)
}

func hasListValue(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func writeSummaryContext(sb *strings.Builder, s SummaryContext) {
	switch s.RecordType {
	case "book_summary":
		sb.WriteString("Book summary\n")
	case "chapter_summary":
		sb.WriteString("Chapter summary")
		if s.ChapterID != "" {
			sb.WriteString(" [")
			sb.WriteString(s.ChapterID)
			sb.WriteString("]")
		}
		if s.ChapterTitle != "" {
			sb.WriteString(" ")
			sb.WriteString(s.ChapterTitle)
		}
		sb.WriteString("\n")
	default:
		sb.WriteString("Summary")
		if s.RecordType != "" {
			sb.WriteString(" [")
			sb.WriteString(s.RecordType)
			sb.WriteString("]")
		}
		sb.WriteString("\n")
	}

	if text := strings.TrimSpace(s.Summary); text != "" {
		sb.WriteString(text)
		sb.WriteString("\n")
	}
	writePromptList(sb, "Themes", s.Themes)
	writePromptList(sb, "Unresolved", s.Unresolved)
	writePromptList(sb, "Supporting references", s.Evidence)
	sb.WriteString("\n")
}

func writePromptList(sb *strings.Builder, label string, values []string) {
	if !hasListValue(values) {
		return
	}
	sb.WriteString(label)
	sb.WriteString(":\n")
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(value)
		sb.WriteString("\n")
	}
}
