// Package project owns the canonical project folder layout and lifecycle.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nusapuksic/story/internal/config"
	"github.com/nusapuksic/story/internal/envconfig"
	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/prompts"
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
	"occurrences.jsonl",
	"claims.jsonl",
	"events.jsonl",
	"character-states.jsonl",
	"unresolved.jsonl",
	"character_roles.jsonl",
	"character_identities.jsonl",
	"summaries.jsonl",
}

var reviewFiles = []string{
	"decisions.jsonl",
}

// InitOptions control project initialization.
type InitOptions struct {
	Title        string
	Language     string
	DefaultModel string
	LLMBaseURL   string
	Force        bool
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
	if err := prompts.WriteDefaults(filepath.Join(dir, PromptsDir)); err != nil {
		return nil, fmt.Errorf("init %s: %w", dir, err)
	}

	cfg := config.Default(ids.NewProjectID(), opts.Title, opts.Language)
	if err := applyInitLLMDefaults(&cfg, opts); err != nil {
		return nil, fmt.Errorf("init %s: %w", dir, err)
	}
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
	if _, err := prompts.SyncDefaults(filepath.Join(dir, PromptsDir)); err != nil {
		return nil, fmt.Errorf("open %s: sync prompts: %w", dir, err)
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
		Title:        opts.Title,
		Language:     opts.Language,
		DefaultModel: opts.DefaultModel,
		LLMBaseURL:   opts.LLMBaseURL,
		Force:        true,
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

// ResetModelFiles clears paragraph-derived model records while preserving the
// canonical model file layout.
func (p *Project) ResetModelFiles() error {
	if err := os.MkdirAll(p.Path(ModelDir), 0o755); err != nil {
		return fmt.Errorf("reset model files: %w", err)
	}
	for _, f := range modelFiles {
		path := p.Path(filepath.Join(ModelDir, f))
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			return fmt.Errorf("reset model file %s: %w", f, err)
		}
	}
	return nil
}

func applyInitLLMDefaults(cfg *config.Config, opts InitOptions) error {
	env, _, _, err := envconfig.Load()
	if err != nil {
		return err
	}

	model := strings.TrimSpace(opts.DefaultModel)
	if model == "" {
		model = env.LLM.DefaultModel
	}
	if model != "" {
		for name, role := range cfg.LLM.Roles {
			role.Model = model
			cfg.LLM.Roles[name] = role
		}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(opts.LLMBaseURL), "/")
	if baseURL == "" {
		baseURL = env.LLM.BaseURL
	}
	if baseURL != "" {
		setDefaultProviderBaseURL(cfg, baseURL)
	}
	if env.LLM.APIKeyEnv != "" {
		setDefaultProviderAPIKeyEnv(cfg, env.LLM.APIKeyEnv)
	}
	if env.LLM.RequestTimeoutSeconds > 0 {
		setDefaultProviderTimeout(cfg, env.LLM.RequestTimeoutSeconds)
	}
	return nil
}

func setDefaultProviderBaseURL(cfg *config.Config, baseURL string) {
	providerName := cfg.LLM.DefaultProvider
	pc := cfg.LLM.Providers[providerName]
	pc.BaseURL = baseURL
	cfg.LLM.Providers[providerName] = pc
}

func setDefaultProviderAPIKeyEnv(cfg *config.Config, apiKeyEnv string) {
	providerName := cfg.LLM.DefaultProvider
	pc := cfg.LLM.Providers[providerName]
	pc.APIKeyEnv = apiKeyEnv
	cfg.LLM.Providers[providerName] = pc
}

func setDefaultProviderTimeout(cfg *config.Config, timeoutSeconds int) {
	providerName := cfg.LLM.DefaultProvider
	pc := cfg.LLM.Providers[providerName]
	pc.RequestTimeoutSeconds = timeoutSeconds
	cfg.LLM.Providers[providerName] = pc
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
