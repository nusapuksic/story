// Command story is the story CLI entry point. Command handlers parse
// arguments, invoke application services, format results, and select exit
// codes; they contain no core import or persistence logic.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/importmd"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/store"
)

var version = "0.1.0-dev"

// Stable exit codes (docs/cli-spec.md §15).
const (
	exitGeneralFailure     = 1
	exitInvalidArguments   = 2
	exitInvalidProject     = 10
	exitAmbiguousImport    = 11
	exitManuscriptConflict = 13
)

type globalFlags struct {
	projectDir string
	jsonOut    bool
	quiet      bool
	verbose    bool
	noColor    bool
}

var flags globalFlags

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(exitCodeFor(err))
	}
}

func exitCodeFor(err error) int {
	switch {
	case errors.Is(err, importmd.ErrAmbiguousOrder):
		return exitAmbiguousImport
	case errors.Is(err, importmd.ErrManuscriptConflict):
		return exitManuscriptConflict
	case errors.Is(err, project.ErrInvalidProject):
		return exitInvalidProject
	case errors.Is(err, errInvalidArguments):
		return exitInvalidArguments
	default:
		return exitGeneralFailure
	}
}

var errInvalidArguments = errors.New("invalid arguments")

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "story",
		Short:         "Compile a fiction manuscript into a layered, source-addressable story model",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&flags.projectDir, "project", ".", "project directory")
	root.PersistentFlags().BoolVar(&flags.jsonOut, "json", false, "emit machine-readable JSON")
	root.PersistentFlags().BoolVar(&flags.quiet, "quiet", false, "suppress nonessential output")
	root.PersistentFlags().BoolVar(&flags.verbose, "verbose", false, "include diagnostic information")
	root.PersistentFlags().BoolVar(&flags.noColor, "no-color", false, "disable terminal colors")

	root.AddCommand(
		newInitCmd(),
		newImportCmd(),
		newIndexCmd(),
		newInspectCmd(),
		newStatusCmd(),
		newDoctorCmd(),
		newStandaloneLLMCmd(),
		newConfigCmd(),
		newCompileCmd(),
	)
	return root
}

// openProject opens the project selected by --project.
func openProject() (*project.Project, error) {
	return project.Open(flags.projectDir)
}

// openIndex opens the project's SQLite index, rebuilding it from canonical
// files when it does not exist yet.
func openIndex(p *project.Project) (*store.Store, error) {
	path := p.Path(project.IndexPath)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := store.Rebuild(p); err != nil {
			return nil, err
		}
	}
	return store.Open(path)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func info(format string, args ...any) {
	if flags.quiet {
		return
	}
	fmt.Printf(format+"\n", args...)
}
