package compiler

import (
	"path/filepath"

	"github.com/nusapuksic/story/internal/project"
	storyprompts "github.com/nusapuksic/story/internal/prompts"
)

func loadCompilerPrompt(p *project.Project, name string) storyprompts.Loaded {
	if p == nil {
		fallback, _ := storyprompts.Default(name)
		return fallback
	}
	loaded, err := storyprompts.Load(p.Path(project.PromptsDir), name)
	if err == nil {
		return loaded
	}
	fallback, _ := storyprompts.Default(name)
	if fallback.Content != "" {
		return fallback
	}
	return storyprompts.Loaded{Name: filepath.Base(name), Version: "unknown"}
}
