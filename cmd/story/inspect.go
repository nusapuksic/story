package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/store"
)

func newInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect indexed project objects",
	}
	cmd.AddCommand(newInspectChapterCmd(), newInspectParagraphCmd(), newInspectSummaryCmd(), newInspectIndexCmd(), newInspectCharacterRolesCmd(), newInspectPrincipalsCmd(), newInspectCharacterIdentitiesCmd(), newInspectNameVariantsCmd())
	return cmd
}

func newInspectCharacterIdentitiesCmd() *cobra.Command {
	return &cobra.Command{Use: "character-identities", Short: "Inspect resolved book-level character identities", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		p, err := openProject()
		if err != nil {
			return err
		}
		records, snapshot, err := compiler.ReadLatestCharacterIdentities(p.Path(filepath.Join(project.ModelDir, "character_identities.jsonl")))
		if err != nil {
			return err
		}
		if snapshot == nil {
			return fmt.Errorf("no character identities found: run 'story compile --layer character-identities'")
		}
		if flags.jsonOut {
			return printJSON(map[string]any{"snapshot": snapshot, "records": records})
		}
		info("Character identities")
		info("Run:  %s", snapshot.RunID)
		info("Hash: %s", snapshot.ArtifactHash)
		for _, r := range records {
			info("")
			info("%s  %s", r.CharacterID, r.CanonicalName)
			printStringList("Source entities", r.SourceEntityIDs)
			printStringList("Aliases", r.Aliases)
			if len(r.Variants) > 0 {
				info("Variants:")
				for _, v := range r.Variants {
					info("  - %s: %s (%s)", v.Type, v.Value, v.Reason)
				}
			}
		}
		return nil
	}}
}

func newInspectNameVariantsCmd() *cobra.Command {
	return &cobra.Command{Use: "name-variants", Short: "Inspect advisory character name variants", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		p, err := openProject()
		if err != nil {
			return err
		}
		records, snapshot, err := compiler.ReadLatestCharacterIdentities(p.Path(filepath.Join(project.ModelDir, "character_identities.jsonl")))
		if err != nil {
			return err
		}
		if snapshot == nil {
			return fmt.Errorf("no character identities found: run 'story compile --layer character-identities'")
		}
		variants := []map[string]any{}
		for _, r := range records {
			for _, v := range r.Variants {
				variants = append(variants, map[string]any{"character_id": r.CharacterID, "canonical_name": r.CanonicalName, "type": v.Type, "value": v.Value, "source_entity_id": v.SourceEntityID, "evidence": v.Evidence, "reason": v.Reason})
			}
		}
		if flags.jsonOut {
			return printJSON(map[string]any{"snapshot": snapshot, "variants": variants})
		}
		info("Name variants")
		if len(variants) == 0 {
			info("No variants.")
			return nil
		}
		for _, v := range variants {
			info("  - %s: %s (%s)", v["canonical_name"], v["value"], v["type"])
		}
		return nil
	}}
}
func newInspectPrincipalsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "principals",
		Short: "Inspect principal character role assessments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspectCharacterRoles(true)
		},
	}
}

func newInspectCharacterRolesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "character-roles",
		Short: "Inspect all character role assessments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspectCharacterRoles(false)
		},
	}
}

func runInspectCharacterRoles(principalsOnly bool) error {
	p, err := openProject()
	if err != nil {
		return err
	}
	records, snapshot, err := compiler.ReadLatestCharacterRoles(p.Path(filepath.Join(project.ModelDir, "character_roles.jsonl")))
	if err != nil {
		return err
	}
	if snapshot == nil {
		return fmt.Errorf("no character roles found: run 'story compile principals'")
	}
	if principalsOnly {
		filtered := records[:0]
		for _, record := range records {
			if record.Classification == compiler.CharacterClassificationPrincipal {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	if flags.jsonOut {
		return printJSON(map[string]any{
			"snapshot": snapshot,
			"records":  records,
		})
	}
	printCharacterRoleRecords(records, snapshot, principalsOnly)
	return nil
}

func printCharacterRoleRecords(records []compiler.CharacterRoleRecord, snapshot *compiler.CharacterRolesSnapshotRecord, principalsOnly bool) {
	if principalsOnly {
		info("Principal characters")
	} else {
		info("Character roles")
	}
	if snapshot != nil {
		info("Run:  %s", snapshot.RunID)
		info("Hash: %s", snapshot.ArtifactHash)
	}
	if len(records) == 0 {
		info("No records.")
		return
	}
	for _, record := range records {
		info("")
		info("%s  %s", record.CharacterID, record.CanonicalName)
		info("Classification: %s", record.Classification)
		if record.Role != "" {
			info("Role:           %s", record.Role)
		}
		info("Confidence:     %.2f", record.Confidence)
		if record.Rationale != "" {
			info("Rationale:      %s", record.Rationale)
		}
		printStringList("Source entities", record.SourceEntityIDs)
		printStringList("Aliases", record.Aliases)
		if len(record.Evidence) > 0 {
			info("")
			info("Evidence:")
			for _, ev := range record.Evidence {
				info("  - %s: %s", ev.SceneID, ev.Reason)
			}
		}
	}
}

func newInspectChapterCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chapter <id>",
		Short: "Inspect a chapter",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := openProject()
			if err != nil {
				return err
			}
			s, err := openIndex(p)
			if err != nil {
				return err
			}
			defer s.Close()
			c, err := s.InspectChapter(args[0])
			if err != nil {
				return err
			}
			if flags.jsonOut {
				return printJSON(map[string]any{
					"id":         c.ID,
					"order":      c.Ordinal,
					"title":      c.Title,
					"file":       c.File,
					"source_key": c.SourceKey,
					"paragraphs": c.ParagraphCount,
				})
			}
			info("Chapter:    %s", c.ID)
			info("Order:      %d", c.Ordinal)
			info("Title:      %s", c.Title)
			info("File:       %s", c.File)
			info("Source:     %s", c.SourceKey)
			info("Paragraphs: %d", c.ParagraphCount)
			return nil
		},
	}
}

func newInspectParagraphCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "paragraph <id>",
		Short: "Inspect a paragraph",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := openProject()
			if err != nil {
				return err
			}
			s, err := openIndex(p)
			if err != nil {
				return err
			}
			defer s.Close()
			row, err := s.InspectParagraph(args[0])
			if err != nil {
				return err
			}
			if flags.jsonOut {
				return printJSON(map[string]any{
					"id":                row.ID,
					"chapter_id":        row.ChapterID,
					"ordinal":           row.Ordinal,
					"block_type":        row.BlockType,
					"text":              row.Text,
					"text_hash":         row.TextHash,
					"source_file":       row.SourceFile,
					"source_line_start": row.SourceLineStart,
					"source_line_end":   row.SourceLineEnd,
				})
			}
			info("Paragraph: %s", row.ID)
			info("Chapter:   %s", row.ChapterID)
			info("Ordinal:   %d", row.Ordinal)
			info("Type:      %s", row.BlockType)
			info("Hash:      %s", row.TextHash)
			info("Source:    %s:%d-%d", row.SourceFile, row.SourceLineStart, row.SourceLineEnd)
			info("")
			info("%s", row.Text)
			return nil
		},
	}
}

func newInspectSummaryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "summary <book|chapter-id>",
		Short: "Inspect generated book or chapter summaries",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := openProject()
			if err != nil {
				return err
			}
			s, err := openIndex(p)
			if err != nil {
				return err
			}
			defer s.Close()
			rec, err := s.InspectSummary(args[0])
			if err != nil {
				return err
			}
			if flags.jsonOut {
				return printJSON(rec)
			}
			printSummaryRecord(rec)
			return nil
		},
	}
}

func newInspectIndexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "index <theme|entity|participant|pov|location|unresolved> <term-or-prefix>",
		Short: "Inspect reverse-index terms and scene-card refs",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := openProject()
			if err != nil {
				return err
			}
			s, err := openIndex(p)
			if err != nil {
				return err
			}
			defer s.Close()

			termType, err := normalizeReverseIndexTermType(args[0])
			if err != nil {
				return err
			}
			query := args[1]
			terms, refs, err := s.InspectReverseIndex(termType, query, 50)
			if err != nil {
				return err
			}
			if flags.jsonOut {
				return printJSON(map[string]any{
					"term_type": termType,
					"query":     query,
					"terms":     terms,
					"refs":      refs,
				})
			}
			if len(refs) > 0 {
				info("Reverse index: %s %q", termType, query)
				info("Occurrences:   %d", len(refs))
				for _, ref := range refs {
					info("  - %s (%s, %s, raw %q)", ref.SceneID, ref.ChapterID, ref.SourceField, ref.RawValue)
				}
				return nil
			}
			if len(terms) > 0 {
				info("Reverse index terms: %s prefix %q", termType, query)
				for _, term := range terms {
					info("  - %s (%d)", term.Term, term.OccurrenceCount)
				}
				return nil
			}
			return fmt.Errorf("no reverse index entries for %s %q", termType, query)
		},
	}
}

func normalizeReverseIndexTermType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "theme", "themes":
		return store.ReverseTermTheme, nil
	case "entity", "entities":
		return store.ReverseTermEntity, nil
	case "participant", "participants":
		return store.ReverseTermParticipant, nil
	case "pov", "povs":
		return store.ReverseTermPOV, nil
	case "location", "locations":
		return store.ReverseTermLocation, nil
	case "unresolved", "question", "questions", "unresolved_question", "unresolved_questions":
		return store.ReverseTermUnresolved, nil
	default:
		return "", fmt.Errorf("unknown reverse index type %q; supported: theme, entity, participant, pov, location, unresolved", value)
	}
}

func printSummaryRecord(rec store.SummaryRow) {
	switch rec.RecordType {
	case "book_summary":
		info("Book summary")
	case "chapter_summary":
		if rec.ChapterTitle != "" {
			info("Chapter summary: %s (%s)", rec.ChapterID, rec.ChapterTitle)
		} else {
			info("Chapter summary: %s", rec.ChapterID)
		}
	default:
		info("Summary: %s", rec.RecordType)
	}
	if rec.GeneratedAt != "" {
		info("Generated: %s", rec.GeneratedAt)
	}
	if rec.GenerationModel != "" {
		info("Model:     %s", rec.GenerationModel)
	}
	info("")
	info("Summary:")
	info("%s", rec.Summary)

	printStringList("Themes", rec.Themes)
	printStringList("Unresolved", rec.Unresolved)
	printStringList("Evidence", rec.Evidence)
	printStringList("Source records", rec.SourceRecords)
}

func printStringList(label string, values []string) {
	if len(values) == 0 {
		return
	}
	info("")
	info("%s:", label)
	for _, value := range values {
		info("  - %s", value)
	}
}
