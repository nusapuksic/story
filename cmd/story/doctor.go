package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/config"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/provider"
)

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check project health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
	cmd.AddCommand(newLLMCmd())
	return cmd
}

// runDoctor checks project-level health and prints a report.
func runDoctor() error {
	issues := 0
	check := func(label string, ok bool, msg string) {
		if ok {
			if !flags.quiet {
				fmt.Printf("  ✓ %s\n", label)
			}
		} else {
			issues++
			fmt.Printf("  ✗ %s: %s\n", label, msg)
		}
	}

	if !flags.quiet {
		fmt.Println("Project health check:")
	}

	// Check project is valid.
	p, err := openProject()
	if err != nil {
		check("project configuration", false, err.Error())
		return fmt.Errorf("doctor: %w", err)
	}
	check("project configuration", true, "")

	// Check canonical directories exist.
	for _, d := range []string{project.ManuscriptDir, project.ModelDir, project.PromptsDir} {
		_, statErr := os.Stat(p.Path(d))
		check(d, statErr == nil, "directory missing")
	}

	// Check manuscript is imported.
	_, tocErr := os.Stat(p.Path(project.TOCPath))
	check("manuscript imported", tocErr == nil, "run 'story import md' first")

	// Check SQLite index.
	idx, idxErr := openIndex(p)
	check("SQLite index", idxErr == nil, fmt.Sprintf("%v", idxErr))
	if idxErr == nil {
		chapters, paragraphs, cntErr := idx.Counts()
		check("manuscript indexed", cntErr == nil && chapters > 0,
			fmt.Sprintf("%d chapters, %d paragraphs", chapters, paragraphs))
		idx.Close()
	}

	// Check prompts.
	for _, name := range []string{"scene-boundaries.md", "scene-extraction.md"} {
		_, statErr := os.Stat(p.Path(project.PromptsDir + "/" + name))
		check("prompt "+name, statErr == nil, "missing; re-run 'story init' or copy from defaults")
	}

	if issues > 0 {
		return fmt.Errorf("doctor: %d issue(s) found", issues)
	}
	if !flags.quiet {
		fmt.Println("All checks passed.")
	}
	return nil
}

// newLLMCmd returns the "llm" sub-command which groups LLM-related commands.
func newLLMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llm",
		Short: "LLM provider utilities",
	}
	cmd.AddCommand(newLLMDoctorCmd())
	return cmd
}

// newLLMDoctorCmd returns the "story doctor llm doctor" or "story llm doctor"
// command.  In the CLI design it is reachable as both:
//
//	story doctor llm doctor
//	story llm doctor   (registered separately in newRootCmd)
func newLLMDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check LLM provider health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLLMDoctor()
		},
	}
}

// runLLMDoctor performs endpoint availability checks for all configured
// providers and reports model availability for configured roles.
func runLLMDoctor() error {
	p, err := openProject()
	if err != nil {
		return err
	}
	llmCfg := p.Config.LLM

	if len(llmCfg.Providers) == 0 {
		fmt.Println("No LLM providers configured in story.toml.")
		fmt.Println("Add an [llm.providers.local] section to enable LLM features.")
		return nil
	}

	issues := 0
	check := func(label string, ok bool, detail string) {
		if ok {
			if !flags.quiet {
				fmt.Printf("  ✓ %s\n", label)
			}
		} else {
			issues++
			if detail != "" {
				fmt.Printf("  ✗ %s: %s\n", label, detail)
			} else {
				fmt.Printf("  ✗ %s\n", label)
			}
		}
	}

	for name, pc := range llmCfg.Providers {
		if !flags.quiet {
			fmt.Printf("\nProvider: %s (%s %s)\n", name, pc.Type, pc.BaseURL)
		}
		isLocal := strings.HasPrefix(pc.BaseURL, "http://127.") ||
			strings.HasPrefix(pc.BaseURL, "http://localhost") ||
			strings.HasPrefix(pc.BaseURL, "http://[::1]")
		remoteMsg := "remote endpoint"
		if pc.APIKeyEnv != "" {
			remoteMsg += " (ensure the API key environment variable is set)"
		}
		check("endpoint type", isLocal, remoteMsg)

		prov := provider.NewOpenAI(pc.BaseURL, pc.APIKeyEnv, pc.RequestTimeoutSeconds)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		models, err := prov.Models(ctx)
		check("endpoint reachable", err == nil, formatErr(err))
		if err != nil {
			continue
		}

		modelIDs := make(map[string]bool, len(models))
		for _, m := range models {
			modelIDs[m.ID] = true
		}
		if !flags.quiet {
			fmt.Printf("  Models available: %d\n", len(models))
		}

		// Check configured role models.
		for role, roleCfg := range llmCfg.Roles {
			if roleCfg.Provider != name {
				continue
			}
			if roleCfg.Model == "" {
				fmt.Printf("  ⚠ role %q: no model configured\n", role)
				continue
			}
			check(fmt.Sprintf("role %q model %q", role, roleCfg.Model),
				modelIDs[roleCfg.Model],
				"model not found in model list")
		}
	}

	if !flags.quiet {
		fmt.Println()
	}
	if issues > 0 {
		return fmt.Errorf("llm doctor: %d issue(s) found", issues)
	}
	if !flags.quiet {
		fmt.Println("All LLM checks passed.")
	}
	return nil
}

// formatErr formats an error for display, or returns empty string when nil.
func formatErr(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "connection timed out"
	}
	return err.Error()
}

// newStandaloneLLMCmd returns the "story llm" top-level command that provides
// "story llm doctor" as a shortcut in addition to "story doctor llm doctor".
func newStandaloneLLMCmd() *cobra.Command {
	llm := &cobra.Command{
		Use:   "llm",
		Short: "LLM provider utilities",
	}
	llm.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Check LLM provider health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLLMDoctor()
		},
	})
	return llm
}

// configShowCmd is added under "config" for §12.1 completeness.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or validate project configuration",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the current project configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := openProject()
			if err != nil {
				return err
			}
			if flags.jsonOut {
				return printJSON(p.Config)
			}
			cfg := p.Config
			info("Version:     %d", cfg.Version)
			info("Project ID:  %s", cfg.ProjectID)
			info("Title:       %s", cfg.Title)
			info("Language:    %s", cfg.Language)
			info("LLM default: %s", cfg.LLM.DefaultProvider)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate the project configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := openProject()
			if err != nil {
				return err
			}
			if err := validateConfig(p.Config); err != nil {
				return err
			}
			info("Configuration is valid.")
			return nil
		},
	})
	return cmd
}

// validateConfig performs basic semantic validation of the config.
func validateConfig(cfg config.Config) error {
	if cfg.Version != 1 {
		return fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if cfg.ProjectID == "" {
		return fmt.Errorf("project_id is required")
	}
	for name, pc := range cfg.LLM.Providers {
		if pc.BaseURL == "" {
			return fmt.Errorf("provider %q: base_url is required", name)
		}
		if pc.Type != "" && pc.Type != "openai-compatible" {
			return fmt.Errorf("provider %q: unsupported type %q", name, pc.Type)
		}
	}
	return nil
}
