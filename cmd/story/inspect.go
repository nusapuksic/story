package main

import (
	"github.com/spf13/cobra"
)

func newInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect indexed project objects",
	}
	cmd.AddCommand(newInspectChapterCmd(), newInspectParagraphCmd())
	return cmd
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
