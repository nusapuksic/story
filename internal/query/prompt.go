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
	entityContext []EntityContext,
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

	if len(entityContext) > 0 {
		sb.WriteString("## Entity context\n\n")
		sb.WriteString("Use this compiled context for character/entity identity, aliases, and scene-scoped appearances. It is not a citation source; cite only paragraph IDs from the evidence paragraphs below.\n\n")
		for _, ctx := range entityContext {
			writeEntityContext(&sb, ctx)
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

func writeEntityContext(sb *strings.Builder, ctx EntityContext) {
	entity := ctx.Entity
	name := strings.TrimSpace(entity.CanonicalName)
	if name == "" {
		return
	}
	if strings.TrimSpace(entity.ID) != "" {
		sb.WriteString("[")
		sb.WriteString(entity.ID)
		sb.WriteString("] ")
	}
	sb.WriteString(name)
	if entityType := strings.TrimSpace(entity.Type); entityType != "" {
		sb.WriteString(" (")
		sb.WriteString(entityType)
		sb.WriteString(")")
	}
	if chapterID := strings.TrimSpace(entity.ChapterID); chapterID != "" {
		sb.WriteString(" - ")
		sb.WriteString(chapterID)
	}
	sb.WriteString("\n")

	if aliases := limitedPromptList(entity.Aliases, defaultEntityListValueLimit); len(aliases) > 0 {
		sb.WriteString("Aliases: ")
		sb.WriteString(strings.Join(aliases, "; "))
		sb.WriteString("\n")
	}
	if len(ctx.Occurrences) > 0 {
		sb.WriteString("Occurrences:\n")
		for _, occ := range ctx.Occurrences {
			sb.WriteString("- ")
			sb.WriteString(occ.SceneID)
			if occ.ChapterID != "" {
				sb.WriteString(" (")
				sb.WriteString(occ.ChapterID)
				sb.WriteString(")")
			}
			if surfaces := limitedPromptList(occ.SurfaceTexts, defaultEntityListValueLimit); len(surfaces) > 0 {
				sb.WriteString(": ")
				sb.WriteString(strings.Join(surfaces, "; "))
			}
			if fields := limitedPromptList(occ.SourceFields, defaultEntityListValueLimit); len(fields) > 0 {
				sb.WriteString(" [")
				sb.WriteString(strings.Join(fields, "; "))
				sb.WriteString("]")
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
}

func limitedPromptList(values []string, limit int) []string {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	out := make([]string, 0, limit)
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	return out
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
