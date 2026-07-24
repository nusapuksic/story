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
	Compile    CompileConfig    `toml:"compile"`
	LLM        LLMConfig        `toml:"llm"`
	Embeddings EmbeddingsConfig `toml:"embeddings"`
}

// ManuscriptConfig configures canonical manuscript handling.
type ManuscriptConfig struct {
	CanonicalFormat   string   `toml:"canonical_format"`
	ChapterBoundary   string   `toml:"chapter_boundary"`
	SceneBreakMarkers []string `toml:"scene_break_markers"`
}

// CompileConfig controls compilation pipeline behaviour.
type CompileConfig struct {
	TargetContextTokens int `toml:"target_context_tokens"`
	// MaximumOutputTokens caps generated output tokens; zero leaves the provider default unchanged.
	MaximumOutputTokens     int     `toml:"maximum_output_tokens"`
	WindowOverlapParagraphs int     `toml:"window_overlap_paragraphs"`
	SceneDetection          string  `toml:"scene_detection"`
	Verification            bool    `toml:"verification"`
	AutoAcceptVerified      bool    `toml:"auto_accept_verified"`
	Temperature             float64 `toml:"temperature"`
}

// LLMProviderConfig configures one LLM provider endpoint.
type LLMProviderConfig struct {
	Type                  string `toml:"type"`
	BaseURL               string `toml:"base_url"`
	APIKeyEnv             string `toml:"api_key_env"`
	RequestTimeoutSeconds int    `toml:"request_timeout_seconds"`
}

// LLMRoleConfig configures one model role (extraction, verification, discussion).
type LLMRoleConfig struct {
	Provider      string `toml:"provider"`
	Model         string `toml:"model"`
	PromptProfile string `toml:"prompt_profile"`
}

// LLMConfig holds all LLM provider and role configuration.
type LLMConfig struct {
	DefaultProvider string                       `toml:"default_provider"`
	Providers       map[string]LLMProviderConfig `toml:"providers"`
	Roles           map[string]LLMRoleConfig     `toml:"roles"`
}

// EmbeddingsConfig configures optional embedding generation.
type EmbeddingsConfig struct {
	Enabled  bool   `toml:"enabled"`
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
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
		Compile: CompileConfig{
			TargetContextTokens:     12000,
			MaximumOutputTokens:     0,
			WindowOverlapParagraphs: 3,
			SceneDetection:          "hybrid",
			Verification:            true,
			AutoAcceptVerified:      false,
			Temperature:             0.1,
		},
		LLM: LLMConfig{
			DefaultProvider: "local",
			Providers: map[string]LLMProviderConfig{
				"local": {
					Type:                  "openai-compatible",
					BaseURL:               "http://127.0.0.1:11434/v1",
					APIKeyEnv:             "",
					RequestTimeoutSeconds: 300,
				},
			},
			Roles: map[string]LLMRoleConfig{
				"extraction": {
					Provider:      "local",
					Model:         "",
					PromptProfile: "conservative",
				},
				"verification": {
					Provider:      "local",
					Model:         "",
					PromptProfile: "strict-evidence",
				},
				"discussion": {
					Provider:      "local",
					Model:         "",
					PromptProfile: "literary-analysis",
				},
			},
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
