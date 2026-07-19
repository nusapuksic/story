// Package config defines the story.toml project configuration schema.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// FileName is the name of the project configuration file.
const FileName = "story.toml"

// Config is the root project configuration stored in story.toml.
type Config struct {
	Version    int              `toml:"version"`
	ProjectID  string           `toml:"project_id"`
	Title      string           `toml:"title"`
	Language   string           `toml:"language"`
	Manuscript ManuscriptConfig `toml:"manuscript"`
}

// ManuscriptConfig configures canonical manuscript handling.
type ManuscriptConfig struct {
	CanonicalFormat   string   `toml:"canonical_format"`
	ChapterBoundary   string   `toml:"chapter_boundary"`
	SceneBreakMarkers []string `toml:"scene_break_markers"`
}

// Default returns the default configuration for a new project.
func Default(projectID, title, language string) Config {
	return Config{
		Version:   1,
		ProjectID: projectID,
		Title:     title,
		Language:  language,
		Manuscript: ManuscriptConfig{
			CanonicalFormat:   "markdown",
			ChapterBoundary:   "hard",
			SceneBreakMarkers: []string{"***", "* * *", "---", "§"},
		},
	}
}

// Load reads story.toml from the given project directory.
func Load(projectDir string) (Config, error) {
	var cfg Config
	path := filepath.Join(projectDir, FileName)
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("load %s: %w", path, err)
	}
	if cfg.Version != 1 {
		return Config{}, fmt.Errorf("load %s: unsupported config version %d", path, cfg.Version)
	}
	return cfg, nil
}

// Save writes the configuration atomically to story.toml in projectDir.
func Save(projectDir string, cfg Config) error {
	path := filepath.Join(projectDir, FileName)
	tmp, err := os.CreateTemp(projectDir, ".story.toml.*")
	if err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if err := toml.NewEncoder(tmp).Encode(cfg); err != nil {
		tmp.Close()
		return fmt.Errorf("save %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	return nil
}
