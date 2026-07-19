package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/store"
)

func newInitCmd() *cobra.Command {
	var opts project.InitOptions
	cmd := &cobra.Command{
		Use:   "init <directory>",
		Short: "Initialize a new story project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			p, err := project.Init(dir, opts)
			if errors.Is(err, project.ErrNotEmpty) {
				return fmt.Errorf("%w: %v", errInvalidArguments, err)
			}
			if err != nil {
				return err
			}
			if err := store.Rebuild(p); err != nil {
				return err
			}
			if flags.jsonOut {
				return printJSON(map[string]string{
					"project_id": p.Config.ProjectID,
					"title":      p.Config.Title,
					"directory":  dir,
				})
			}
			info("Initialized project %q (%s) in %s", p.Config.Title, p.Config.ProjectID, dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Title, "title", "", "project title")
	cmd.Flags().StringVar(&opts.Language, "language", "en", "project language code")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "initialize even when the destination is nonempty")
	return cmd
}
