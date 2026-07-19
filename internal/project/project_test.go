package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
