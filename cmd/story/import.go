package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/importmd"
	"github.com/nusapuksic/story/internal/project"
)

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a manuscript source",
	}
	cmd.AddCommand(newImportMDCmd(), newImportReportCmd())
	return cmd
}

func newImportMDCmd() *cobra.Command {
	var opts importmd.Options
	cmd := &cobra.Command{
		Use:   "md <path>",
		Short: "Import Markdown from a chapter folder or continuous file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, initialized, err := project.OpenOrInit(flags.projectDir, project.InitOptions{Title: opts.Title})
			if err != nil {
				return err
			}
			if initialized && !flags.jsonOut {
				info("Initialized project %q (%s) in %s", p.Config.Title, p.Config.ProjectID, flags.projectDir)
			}
			res, err := importmd.Run(p, args[0], opts)
			if err != nil {
				return err
			}
			if flags.jsonOut {
				return printJSON(map[string]any{
					"run_id":     res.RunID,
					"chapters":   res.Chapters,
					"paragraphs": res.Paragraphs,
					"warnings":   res.Warnings,
					"dry_run":    res.DryRun,
				})
			}
			if res.DryRun {
				info("Dry run %s: %d chapters, %d paragraphs would be imported", res.RunID, res.Chapters, res.Paragraphs)
			} else {
				info("Imported %d chapters, %d paragraphs (run %s)", res.Chapters, res.Paragraphs, res.RunID)
			}
			for _, w := range res.Warnings {
				fmt.Fprintln(os.Stderr, "Warning:", w)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.TOC, "toc", "", "explicit source manifest path")
	cmd.Flags().StringVar(&opts.Pattern, "pattern", "*.md", "source file glob")
	cmd.Flags().StringVar(&opts.Title, "title", "", "project title override")
	cmd.Flags().BoolVar(&opts.Replace, "replace", false, "replace an existing canonical manuscript")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "detect and report without modifying the manuscript")
	cmd.Flags().IntVar(&opts.ChapterHeadingLevel, "chapter-heading-level", 1, "heading level for splitting a continuous Markdown file")
	cmd.Flags().StringVar(&opts.ChapterRegex, "chapter-regex", "", "line regex for chapter boundaries in a continuous Markdown file")
	cmd.Flags().BoolVar(&opts.SingleChapter, "single-chapter", false, "import a continuous Markdown file as one chapter")
	return cmd
}

func newImportReportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "report [<run-id>]",
		Short: "Show an import report",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := openProject()
			if err != nil {
				return err
			}
			recordsDir := p.Path(project.ImportRecordsDir)
			runID := ""
			if len(args) == 1 {
				runID = args[0]
			} else {
				entries, err := os.ReadDir(recordsDir)
				if err != nil {
					return fmt.Errorf("read import records: %w", err)
				}
				var runs []string
				for _, e := range entries {
					if e.IsDir() {
						runs = append(runs, e.Name())
					}
				}
				if len(runs) == 0 {
					return errors.New("no import records found")
				}
				sort.Strings(runs)
				runID = runs[len(runs)-1]
			}
			data, err := os.ReadFile(filepath.Join(recordsDir, runID, "report.json"))
			if err != nil {
				return fmt.Errorf("read import report %s: %w", runID, err)
			}
			if flags.jsonOut {
				fmt.Print(string(data))
				return nil
			}
			var r importmd.Report
			if err := json.Unmarshal(data, &r); err != nil {
				return fmt.Errorf("parse import report %s: %w", runID, err)
			}
			info("Run:         %s", r.RunID)
			info("Type:        %s", r.Type)
			info("Source:      %s", r.SourcePath)
			info("Imported at: %s", r.ImportedAt)
			info("Chapters:    %d", r.Chapters)
			info("Paragraphs:  %d", r.Paragraphs)
			info("Status:      %s", r.Status)
			for _, w := range r.Warnings {
				info("Warning:     %s", w)
			}
			return nil
		},
	}
}
