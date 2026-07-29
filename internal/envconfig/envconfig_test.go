package envconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingConfigIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.toml")
	t.Setenv(OverrideEnv, path)

	cfg, found, gotPath, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("found = true, want false")
	}
	if gotPath != path {
		t.Fatalf("path = %q, want %q", gotPath, path)
	}
	if cfg.LLM.DefaultModel != "" || cfg.LLM.BaseURL != "" {
		t.Fatalf("cfg = %+v, want empty", cfg)
	}
}

func TestLoadReadsLLMDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.toml")
	data := []byte(`
[llm]
default_model = "llama3.1:8b"
base_url = "http://192.168.1.50:11434/v1/"
api_key_env = "STORY_LLM_API_KEY"
request_timeout_seconds = 120
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(OverrideEnv, path)

	cfg, found, gotPath, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if gotPath != path {
		t.Fatalf("path = %q, want %q", gotPath, path)
	}
	if cfg.LLM.DefaultModel != "llama3.1:8b" {
		t.Fatalf("default model = %q", cfg.LLM.DefaultModel)
	}
	if cfg.LLM.BaseURL != "http://192.168.1.50:11434/v1" {
		t.Fatalf("base url = %q", cfg.LLM.BaseURL)
	}
	if cfg.LLM.APIKeyEnv != "STORY_LLM_API_KEY" {
		t.Fatalf("api key env = %q", cfg.LLM.APIKeyEnv)
	}
	if cfg.LLM.RequestTimeoutSeconds != 120 {
		t.Fatalf("timeout = %d", cfg.LLM.RequestTimeoutSeconds)
	}
}

func TestLoadReadsEnvTOMLFromWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	data := []byte("[llm]\ndefault_model = \"root-model\"\nbase_url = \"http://10.0.0.5:11434/v1\"\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv(OverrideEnv, "")

	cfg, found, gotPath, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if gotPath != path {
		t.Fatalf("path = %q, want %q", gotPath, path)
	}
	if cfg.LLM.DefaultModel != "root-model" {
		t.Fatalf("default model = %q", cfg.LLM.DefaultModel)
	}
	if cfg.LLM.BaseURL != "http://10.0.0.5:11434/v1" {
		t.Fatalf("base url = %q", cfg.LLM.BaseURL)
	}
}
