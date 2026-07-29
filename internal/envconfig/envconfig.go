// Package envconfig reads environment defaults that seed new story projects.
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
	// path to the environment config file.
	OverrideEnv = "STORY_ENV_CONFIG"
	// FileName is the environment config filename.
	FileName = "env.toml"
)

// Config holds environment defaults copied into newly initialized projects.
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

// DefaultPath returns the config file path. STORY_ENV_CONFIG wins. Otherwise,
// story first uses env.toml in the current directory, then env.toml beside the
// executable, and finally reports the current-directory path even when missing.
func DefaultPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv(OverrideEnv)); path != "" {
		return path, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		path := filepath.Join(cwd, FileName)
		if fileExists(path) {
			return path, nil
		}
	}
	if exe, err := os.Executable(); err == nil {
		path := filepath.Join(filepath.Dir(exe), FileName)
		if fileExists(path) {
			return path, nil
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, FileName), nil
	}
	return FileName, nil
}

// Load reads the environment config. Missing files are not an error.
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (cfg *Config) normalize() {
	cfg.LLM.DefaultModel = strings.TrimSpace(cfg.LLM.DefaultModel)
	cfg.LLM.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.LLM.BaseURL), "/")
	cfg.LLM.APIKeyEnv = strings.TrimSpace(cfg.LLM.APIKeyEnv)
}
