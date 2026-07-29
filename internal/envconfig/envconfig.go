// Package envconfig reads user-level defaults that seed new story projects.
package envconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	// OverrideEnv names an optional environment variable containing the exact
	// path to the user environment config file.
	OverrideEnv = "STORY_ENV_CONFIG"
	// AppDir is the application directory under the OS user config directory.
	AppDir = "story"
	// FileName is the user environment config filename.
	FileName = "env.toml"
)

// Config holds user-level defaults copied into newly initialized projects.
type Config struct {
	LLM LLMConfig `toml:"llm"`
}

// LLMConfig holds defaults for the generated local OpenAI-compatible provider.
type LLMConfig struct {
	DefaultModel          string `toml:"default_model"`
	BaseURL               string `toml:"base_url"`
	APIKeyEnv             string `toml:"api_key_env"`
	RequestTimeoutSeconds int    `toml:"request_timeout_seconds"`
}

// DefaultPath returns the config file path. STORY_ENV_CONFIG, when set, wins.
func DefaultPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv(OverrideEnv)); path != "" {
		return path, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, AppDir, FileName), nil
}

// Load reads the user environment config. Missing files are not an error.
func Load() (Config, bool, string, error) {
	path, err := DefaultPath()
	if err != nil {
		return Config{}, false, "", nil
	}
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			return Config{}, false, path, nil
		}
		return Config{}, false, path, fmt.Errorf("load %s: %w", path, err)
	}
	cfg.normalize()
	return cfg, true, path, nil
}

func (cfg *Config) normalize() {
	cfg.LLM.DefaultModel = strings.TrimSpace(cfg.LLM.DefaultModel)
	cfg.LLM.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.LLM.BaseURL), "/")
	cfg.LLM.APIKeyEnv = strings.TrimSpace(cfg.LLM.APIKeyEnv)
}
