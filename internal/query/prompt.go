package query

import (
	"fmt"
	"strings"
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

// buildUserPrompt constructs the user-turn message including higher-level
// records, scene context, evidence paragraphs, and the question.
func buildUserPrompt(question, mode string, packet evidencePacket) string {
	var sb strings.Builder

	if hasVisibleSummaryContext(packet.Summaries) {
		sb.WriteString("## Summary context\n\n")
		sb.WriteString("Use these generated summaries for high-level orientation. They are valid higher-level records for records_used; cite paragraph IDs only from the evidence paragraphs below.\n\n")
		for _, s := range packet.Summaries {
			if !isVisibleSummaryContext(s) {
				continue
			}
			writeSummaryContext(&sb, s)
		}
	}

	if len(packet.CharacterRoles) > 0 {
		sb.WriteString("## Character role context\n\n")
		sb.WriteString("Use these compiled character-role records for principal/major/supporting roles, aliases, and character function. Include relied-on role IDs in records_used.\n\n")
		for _, role := range packet.CharacterRoles {
			writeCharacterRoleContext(&sb, role)
		}
	}

	if len(packet.EntityContext) > 0 {
		sb.WriteString("## Entity context\n\n")
		sb.WriteString("Use this compiled context for character/entity identity, aliases, and scene-scoped appearances. Include relied-on entity IDs in records_used; cite paragraph IDs only from the evidence paragraphs below.\n\n")
		for _, ctx := range packet.EntityContext {
			writeEntityContext(&sb, ctx)
		}
	}

	if len(packet.Digests) > 0 {
		sb.WriteString("## Condensed evidence\n\n")
		sb.WriteString("These digests were generated earlier in this ask run from paragraph evidence. They are higher-level records for records_used, not direct paragraph citations.\n\n")
		for _, digest := range packet.Digests {
			writeEvidenceDigest(&sb, digest)
		}
	}

	if len(packet.SceneCards) > 0 {
		sb.WriteString("## Scene context\n\n")
		sb.WriteString("Use these scene records for broader chronology and structure. Include relied-on scene IDs in records_used.\n\n")
		for _, c := range packet.SceneCards {
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
	if len(packet.Paragraphs) == 0 {
		sb.WriteString("(none)\n\n")
	} else {
		for _, p := range packet.Paragraphs {
			sb.WriteString("[")
			sb.WriteString(p.ID)
			sb.WriteString("] (")
			sb.WriteString(p.ChapterID)
			sb.WriteString(")\n")
			sb.WriteString(p.Text)
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("## Question\n\n")
	sb.WriteString(question)
	sb.WriteString("\n\n")
	sb.WriteString("Answer in JSON as specified. Cite only paragraph IDs listed in Evidence paragraphs. Put any summary, character role, entity, scene, or digest IDs you relied on in records_used.")

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
	return strings.TrimSpace(s.Summary) != "" || hasListValue(s.Themes) || hasListValue(s.Unresolved) || len(s.CharacterFinalStates) > 0
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

func writeCharacterRoleContext(sb *strings.Builder, role CharacterRoleContext) {
	id := characterRoleRecordID(role)
	name := strings.TrimSpace(role.CanonicalName)
	if id == "" || name == "" {
		return
	}
	sb.WriteString("[")
	sb.WriteString(id)
	sb.WriteString("] ")
	sb.WriteString(name)
	if class := strings.TrimSpace(role.Classification); class != "" {
		sb.WriteString(" (")
		sb.WriteString(class)
		if roleText := strings.TrimSpace(role.Role); roleText != "" {
			sb.WriteString("; ")
			sb.WriteString(roleText)
		}
		sb.WriteString(")")
	}
	sb.WriteString("\n")
	if aliases := limitedPromptList(role.Aliases, defaultEntityListValueLimit); len(aliases) > 0 {
		sb.WriteString("Aliases: ")
		sb.WriteString(strings.Join(aliases, "; "))
		sb.WriteString("\n")
	}
	if len(role.SourceEntityIDs) > 0 {
		sb.WriteString("Source entities: ")
		sb.WriteString(strings.Join(limitedPromptList(role.SourceEntityIDs, defaultEntityListValueLimit), "; "))
		sb.WriteString("\n")
	}
	if role.Confidence > 0 {
		fmt.Fprintf(sb, "Confidence: %.2f\n", role.Confidence)
	}
	if rationale := strings.TrimSpace(role.Rationale); rationale != "" {
		sb.WriteString("Rationale: ")
		sb.WriteString(rationale)
		sb.WriteString("\n")
	}
	if len(role.Evidence) > 0 {
		sb.WriteString("Scene evidence:\n")
		for _, ev := range role.Evidence {
			if strings.TrimSpace(ev.SceneID) == "" {
				continue
			}
			sb.WriteString("- ")
			sb.WriteString(strings.TrimSpace(ev.SceneID))
			if reason := strings.TrimSpace(ev.Reason); reason != "" {
				sb.WriteString(": ")
				sb.WriteString(reason)
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
}

func writeEvidenceDigest(sb *strings.Builder, digest EvidenceDigest) {
	id := strings.TrimSpace(digest.ID)
	if id == "" || strings.TrimSpace(digest.Summary) == "" {
		return
	}
	sb.WriteString("[")
	sb.WriteString(id)
	sb.WriteString("]")
	if scope := strings.TrimSpace(digest.Scope); scope != "" {
		sb.WriteString(" ")
		sb.WriteString(scope)
	}
	sb.WriteString("\n")
	sb.WriteString(digest.Summary)
	sb.WriteString("\n")
	writePromptList(sb, "Support", digest.Support)
	writePromptList(sb, "Uncertainties", digest.Uncertainties)
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
	sb.WriteString("[")
	sb.WriteString(summaryRecordID(s))
	sb.WriteString("] ")
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
	if len(s.CharacterFinalStates) > 0 {
		sb.WriteString("Character final states:\n")
		for _, state := range s.CharacterFinalStates {
			characterID := strings.TrimSpace(state.CharacterID)
			stateText := strings.TrimSpace(state.State)
			if characterID == "" || stateText == "" {
				continue
			}
			sb.WriteString("- ")
			sb.WriteString(characterID)
			sb.WriteString(": ")
			sb.WriteString(stateText)
			sb.WriteString("\n")
		}
	}
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
