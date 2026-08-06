// Command story is the story CLI entry point. Command handlers parse
// arguments, invoke application services, format results, and select exit
// codes; they contain no core import or persistence logic.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/importmd"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/store"
)

var version = "dev"

// Stable exit codes (docs/cli-spec.md §15).
const (
	exitGeneralFailure       = 1
	exitInvalidArguments     = 2
	exitInvalidProject       = 10
	exitAmbiguousImport      = 11
	exitManuscriptConflict   = 13
	exitInsufficientEvidence = 40
)

type globalFlags struct {
	projectDir string
	jsonOut    bool
	quiet      bool
	verbose    bool
	noColor    bool
}

var flags globalFlags

const terminalTimestampFormat = "2006-01-02 15:04:05"

var terminalNow = time.Now

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	root := newRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		terminalErr("Error: %v", err)
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
	case isInsufficientEvidence(err):
		return exitInsufficientEvidence
	default:
		return exitGeneralFailure
	}
}

// isInsufficientEvidence reports whether err is an insufficientEvidenceError.
func isInsufficientEvidence(err error) bool {
	var e *insufficientEvidenceError
	return errors.As(err, &e)
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
		newSearchCmd(),
		newAskCmd(),
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
	terminalOut(format, args...)
}

func terminalOut(format string, args ...any) {
	terminalPrint(os.Stdout, format, args...)
}

func terminalErr(format string, args ...any) {
	terminalPrint(os.Stderr, format, args...)
}

func terminalPrint(w io.Writer, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprint(w, formatTerminalOutput(msg, terminalNow())+"\n")
}

func formatTerminalOutput(msg string, ts time.Time) string {
	prefix := "[" + ts.Local().Format(terminalTimestampFormat) + "] "
	lines := strings.Split(msg, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
