package main

import (
	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/store"
)

func newIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Manage the rebuildable SQLite index",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "rebuild",
		Short: "Rebuild the SQLite index from the canonical project files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := openProject()
			if err != nil {
				return err
			}
			if err := store.Rebuild(p); err != nil {
				return err
			}
			info("Index rebuilt from canonical project files")
			return nil
		},
	})
	return cmd
}
