// Package project owns the canonical project folder layout and lifecycle.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nusapuksic/story/internal/config"
	"github.com/nusapuksic/story/internal/ids"
)

// Sentinel errors mapped to CLI exit codes by the command layer.
var (
	// ErrNotEmpty is returned by Init when the destination is nonempty
	// and --force was not supplied.
	ErrNotEmpty = errors.New("destination directory is not empty")
	// ErrInvalidProject is returned by Open when the directory is not a
	// valid story project.
	ErrInvalidProject = errors.New("invalid project")
)

// Project is an opened story project.
type Project struct {
	Dir    string
	Config config.Config
}

// Canonical directory layout, relative to the project root.
const (
	SourceOriginalDir = "source/original"
	ImportRecordsDir  = "source/import-records"
	ManuscriptDir     = "manuscript"
	ChaptersDir       = "manuscript/chapters"
	ModelDir          = "model"
	ReviewsDir        = "reviews"
	PromptsDir        = "prompts"
	StoryDir          = ".story"
	CacheDir          = ".story/cache"
	RunsDir           = ".story/runs"
	LocksDir          = ".story/locks"
	LogsDir           = ".story/logs"
	IndexPath         = ".story/index.sqlite"
	TOCPath           = "manuscript/toc.toml"
)

var canonicalDirs = []string{
	SourceOriginalDir,
	ImportRecordsDir,
	ChaptersDir,
	ModelDir,
	ReviewsDir,
	PromptsDir,
	CacheDir,
	RunsDir,
	LocksDir,
	LogsDir,
}

var modelFiles = []string{
	"scenes.jsonl",
	"entities.jsonl",
	"mentions.jsonl",
	"claims.jsonl",
	"events.jsonl",
	"character-states.jsonl",
	"unresolved.jsonl",
	"summaries.jsonl",
}

var reviewFiles = []string{
	"decisions.jsonl",
}

// InitOptions control project initialization.
type InitOptions struct {
	Title    string
	Language string
	Force    bool
}

// Init creates a new project in dir. It fails with ErrNotEmpty when the
// destination exists and is nonempty, unless opts.Force is set.
func Init(dir string, opts InitOptions) (*Project, error) {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// created below
	case err != nil:
		return nil, fmt.Errorf("init %s: %w", dir, err)
	case len(entries) > 0 && !opts.Force:
		return nil, fmt.Errorf("init %s: %w (use --force to initialize anyway)", dir, ErrNotEmpty)
	}

	if opts.Language == "" {
		opts.Language = "en"
	}
	if opts.Title == "" {
		opts.Title = filepath.Base(absOrSelf(dir))
	}

	for _, d := range canonicalDirs {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return nil, fmt.Errorf("init %s: %w", dir, err)
		}
	}
	for _, f := range modelFiles {
		if err := touch(filepath.Join(dir, ModelDir, f)); err != nil {
			return nil, fmt.Errorf("init %s: %w", dir, err)
		}
	}
	for _, f := range reviewFiles {
		if err := touch(filepath.Join(dir, ReviewsDir, f)); err != nil {
			return nil, fmt.Errorf("init %s: %w", dir, err)
		}
	}
	if err := writeDefaultPrompts(filepath.Join(dir, PromptsDir)); err != nil {
		return nil, fmt.Errorf("init %s: %w", dir, err)
	}

	cfg := config.Default(ids.NewProjectID(), opts.Title, opts.Language)
	if err := config.Save(dir, cfg); err != nil {
		return nil, fmt.Errorf("init %s: %w", dir, err)
	}
	return &Project{Dir: dir, Config: cfg}, nil
}

// Open loads an existing project rooted at dir.
func Open(dir string) (*Project, error) {
	if _, err := os.Stat(filepath.Join(dir, config.FileName)); err != nil {
		return nil, fmt.Errorf("open %s: %w: missing %s", dir, ErrInvalidProject, config.FileName)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w: %v", dir, ErrInvalidProject, err)
	}
	return &Project{Dir: dir, Config: cfg}, nil
}

// OpenOrInit loads an existing project rooted at dir, or initializes the
// canonical layout when story.toml is missing.
func OpenOrInit(dir string, opts InitOptions) (*Project, bool, error) {
	if _, err := os.Stat(filepath.Join(dir, config.FileName)); err == nil {
		p, err := Open(dir)
		return p, false, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("open %s: %w: %v", dir, ErrInvalidProject, err)
	}

	p, err := Init(dir, InitOptions{
		Title:    opts.Title,
		Language: opts.Language,
		Force:    true,
	})
	if err != nil {
		return nil, false, err
	}
	return p, true, nil
}

// Path returns an absolute path inside the project.
func (p *Project) Path(rel string) string {
	return filepath.Join(p.Dir, rel)
}

func touch(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func absOrSelf(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}
