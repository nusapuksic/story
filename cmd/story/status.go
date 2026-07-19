package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/store"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report project status",
		Args:  cobra.NoArgs,
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
			chapters, paragraphs, err := s.Counts()
			if err != nil {
				return err
			}
			lastImport := ""
			if r, err := s.LastImport(); err == nil {
				lastImport = r.RunID
			} else if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if flags.jsonOut {
				return printJSON(map[string]any{
					"title":       p.Config.Title,
					"project_id":  p.Config.ProjectID,
					"chapters":    chapters,
					"paragraphs":  paragraphs,
					"last_import": lastImport,
				})
			}
			info("Title:       %s", p.Config.Title)
			info("Project ID:  %s", p.Config.ProjectID)
			info("Chapters:    %d", chapters)
			info("Paragraphs:  %d", paragraphs)
			if lastImport != "" {
				info("Last import: %s", lastImport)
			} else {
				info("Last import: none")
			}
			return nil
		},
	}
}
