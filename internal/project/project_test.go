package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	storyprompts "github.com/nusapuksic/story/internal/prompts"
)

func TestMain(m *testing.M) {
	path := filepath.Join(os.TempDir(), "story-test-missing-env.toml")
	_ = os.Remove(path)
	_ = os.Setenv("STORY_ENV_CONFIG", path)
	os.Exit(m.Run())
}

func TestInitCreatesLayout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "novel")
	p, err := Init(dir, InitOptions{Title: "My Novel", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Config.ProjectID == "" {
		t.Error("project_id not generated")
	}
	if p.Config.Title != "My Novel" {
		t.Errorf("title = %q", p.Config.Title)
	}
	if p.Config.Compile.VerificationMode != "all" {
		t.Errorf("verification_mode = %q, want all", p.Config.Compile.VerificationMode)
	}
	for _, d := range []string{
		"story.toml",
		"source/original", "source/import-records",
		"manuscript/chapters",
		"model/scenes.jsonl", "reviews/decisions.jsonl",
		"prompts/scene-boundaries.md", "prompts/answer-question.md",
		".story/cache", ".story/runs", ".story/locks", ".story/logs",
	} {
		if _, err := os.Stat(filepath.Join(dir, d)); err != nil {
			t.Errorf("missing %s: %v", d, err)
		}
	}

	got, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.ProjectID != p.Config.ProjectID {
		t.Errorf("reopened project_id = %q, want %q", got.Config.ProjectID, p.Config.ProjectID)
	}
}

func TestOpenSyncsOlderPromptDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "novel")
	created, err := Init(dir, InitOptions{Title: "Prompted Novel"})
	if err != nil {
		t.Fatal(err)
	}

	promptPath := filepath.Join(dir, PromptsDir, storyprompts.BookSummary)
	oldPrompt := "<!-- prompt_version: book-summary-v1 -->\n\nold local summary prompt\n"
	if err := os.WriteFile(promptPath, []byte(oldPrompt), 0o644); err != nil {
		t.Fatal(err)
	}

	opened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Config.ProjectID != created.Config.ProjectID {
		t.Errorf("project_id = %q, want %q", opened.Config.ProjectID, created.Config.ProjectID)
	}

	data, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if version := storyprompts.VersionFromText(content); version != "book-summary-v2" {
		t.Fatalf("book summary prompt version = %q, want book-summary-v2\n%s", version, content)
	}
	if strings.Contains(content, "old local summary prompt") {
		t.Fatalf("older project prompt was not replaced:\n%s", content)
	}
}
func TestInitDefaultModelPopulatesLLMRoles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "novel")
	p, err := Init(dir, InitOptions{Title: "My Novel", DefaultModel: "llama3.1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"extraction", "verification", "discussion"} {
		role, ok := p.Config.LLM.Roles[name]
		if !ok {
			t.Fatalf("missing role %q", name)
		}
		if role.Model != "llama3.1" {
			t.Errorf("role %s model = %q, want llama3.1", name, role.Model)
		}
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Config.LLM.Roles["discussion"].Model != "llama3.1" {
		t.Errorf("reopened discussion model = %q, want llama3.1", reopened.Config.LLM.Roles["discussion"].Model)
	}
}

func TestInitUsesEnvConfigDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.toml")
	data := []byte(`
[llm]
default_model = "llama3.1:8b"
base_url = "http://192.168.1.50:11434/v1"
request_timeout_seconds = 180
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STORY_ENV_CONFIG", path)

	dir := filepath.Join(t.TempDir(), "novel")
	p, err := Init(dir, InitOptions{Title: "My Novel"})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"extraction", "verification", "discussion"} {
		role := p.Config.LLM.Roles[name]
		if role.Model != "llama3.1:8b" {
			t.Errorf("role %s model = %q, want llama3.1:8b", name, role.Model)
		}
	}
	local := p.Config.LLM.Providers["local"]
	if local.BaseURL != "http://192.168.1.50:11434/v1" {
		t.Errorf("base URL = %q, want LAN server", local.BaseURL)
	}
	if local.RequestTimeoutSeconds != 180 {
		t.Errorf("timeout = %d, want 180", local.RequestTimeoutSeconds)
	}
}

func TestInitOptionsOverrideEnvConfigDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.toml")
	data := []byte(`
[llm]
default_model = "env-model"
base_url = "http://192.168.1.50:11434/v1"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STORY_ENV_CONFIG", path)

	dir := filepath.Join(t.TempDir(), "novel")
	p, err := Init(dir, InitOptions{
		Title:        "My Novel",
		DefaultModel: "flag-model",
		LLMBaseURL:   "http://10.0.0.5:1234/v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	if p.Config.LLM.Roles["discussion"].Model != "flag-model" {
		t.Errorf("discussion model = %q, want flag-model", p.Config.LLM.Roles["discussion"].Model)
	}
	if p.Config.LLM.Providers["local"].BaseURL != "http://10.0.0.5:1234/v1" {
		t.Errorf("base URL = %q, want explicit flag URL", p.Config.LLM.Providers["local"].BaseURL)
	}
}

func TestOpenOrInitInitializesWithDefaultModel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "generated")
	p, initialized, err := OpenOrInit(dir, InitOptions{DefaultModel: "mistral"})
	if err != nil {
		t.Fatal(err)
	}
	if !initialized {
		t.Fatal("initialized = false, want true")
	}
	if p.Config.LLM.Roles["extraction"].Model != "mistral" {
		t.Errorf("extraction model = %q, want mistral", p.Config.LLM.Roles["extraction"].Model)
	}
}

func TestInitFailsOnNonemptyWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir, InitOptions{}); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("err = %v, want ErrNotEmpty", err)
	}
	if _, err := Init(dir, InitOptions{Force: true}); err != nil {
		t.Fatalf("force init failed: %v", err)
	}
}

func TestOpenRejectsNonProject(t *testing.T) {
	if _, err := Open(t.TempDir()); !errors.Is(err, ErrInvalidProject) {
		t.Fatalf("err = %v, want ErrInvalidProject", err)
	}
}

func TestOpenOrInitInitializesMissingProject(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "generated")
	p, initialized, err := OpenOrInit(dir, InitOptions{Title: "Generated Novel"})
	if err != nil {
		t.Fatal(err)
	}
	if !initialized {
		t.Fatal("initialized = false, want true")
	}
	if p.Config.Title != "Generated Novel" {
		t.Errorf("title = %q, want Generated Novel", p.Config.Title)
	}
	if _, err := os.Stat(filepath.Join(dir, "story.toml")); err != nil {
		t.Fatalf("story.toml was not generated: %v", err)
	}
}

func TestOpenOrInitPreservesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	draft := filepath.Join(dir, "draft.md")
	if err := os.WriteFile(draft, []byte("# Chapter One\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, initialized, err := OpenOrInit(dir, InitOptions{}); err != nil {
		t.Fatal(err)
	} else if !initialized {
		t.Fatal("initialized = false, want true")
	}

	got, err := os.ReadFile(draft)
	if err != nil {
		t.Fatalf("existing draft was not preserved: %v", err)
	}
	if string(got) != "# Chapter One\n" {
		t.Errorf("draft content = %q", got)
	}
}

func TestOpenOrInitOpensExistingProject(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "novel")
	created, err := Init(dir, InitOptions{Title: "Existing Novel"})
	if err != nil {
		t.Fatal(err)
	}

	opened, initialized, err := OpenOrInit(dir, InitOptions{Title: "Ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if initialized {
		t.Fatal("initialized = true, want false")
	}
	if opened.Config.ProjectID != created.Config.ProjectID {
		t.Errorf("project_id = %q, want %q", opened.Config.ProjectID, created.Config.ProjectID)
	}
	if opened.Config.Title != "Existing Novel" {
		t.Errorf("title = %q, want Existing Novel", opened.Config.Title)
	}
}
