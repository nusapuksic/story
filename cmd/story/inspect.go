package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/project"
)

func newInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect indexed project objects",
	}
	cmd.AddCommand(newInspectChapterCmd(), newInspectParagraphCmd(), newInspectSummaryCmd())
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
			rec, err := latestSummaryRecord(p, args[0])
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

func latestSummaryRecord(p *project.Project, target string) (compiler.SummaryRecord, error) {
	target = strings.TrimSpace(target)
	path := p.Path(filepath.Join(project.ModelDir, "summaries.jsonl"))
	f, err := os.Open(path)
	if err != nil {
		return compiler.SummaryRecord{}, fmt.Errorf("read summaries: %w", err)
	}
	defer f.Close()

	wantBook := target == "book" || target == "book_summary"
	var latest compiler.SummaryRecord
	found := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec compiler.SummaryRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if wantBook {
			if rec.RecordType != "book_summary" {
				continue
			}
		} else if rec.RecordType != "chapter_summary" || rec.ChapterID != target {
			continue
		}
		latest = rec
		found = true
	}
	if err := sc.Err(); err != nil {
		return compiler.SummaryRecord{}, fmt.Errorf("read summaries: %w", err)
	}
	if !found {
		if wantBook {
			return compiler.SummaryRecord{}, fmt.Errorf("no book summary found: run 'story compile --layer summaries'")
		}
		return compiler.SummaryRecord{}, fmt.Errorf("no chapter summary found for %s: run 'story compile --layer summaries'", target)
	}
	return latest, nil
}

func printSummaryRecord(rec compiler.SummaryRecord) {
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
	if rec.Generation.GeneratedAt != "" {
		info("Generated: %s", rec.Generation.GeneratedAt)
	}
	if rec.Generation.Model != "" {
		info("Model:     %s", rec.Generation.Model)
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
