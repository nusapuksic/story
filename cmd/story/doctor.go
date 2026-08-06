package main

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/config"
	"github.com/nusapuksic/story/internal/project"
	storyprompts "github.com/nusapuksic/story/internal/prompts"
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
				info("  ✓ %s", label)
			}
		} else {
			issues++
			terminalOut("  ✗ %s: %s", label, msg)
		}
	}

	if !flags.quiet {
		info("Project health check:")
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
	for _, name := range storyprompts.Names() {
		_, statErr := os.Stat(p.Path(project.PromptsDir + "/" + name))
		check("prompt "+name, statErr == nil, "missing; re-run 'story init' or copy from defaults")
	}

	if issues > 0 {
		return fmt.Errorf("doctor: %d issue(s) found", issues)
	}
	if !flags.quiet {
		info("All checks passed.")
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
		terminalOut("No LLM providers configured in story.toml.")
		terminalOut("Add an [llm.providers.local] section to enable LLM features.")
		return nil
	}

	issues := 0
	check := func(label string, ok bool, detail string) {
		if ok {
			if !flags.quiet {
				info("  ✓ %s", label)
			}
		} else {
			issues++
			if detail != "" {
				terminalOut("  ✗ %s: %s", label, detail)
			} else {
				terminalOut("  ✗ %s", label)
			}
		}
	}

	for name, pc := range llmCfg.Providers {
		if !flags.quiet {
			info("\nProvider: %s (%s %s)", name, pc.Type, pc.BaseURL)
		}
		scope, detail := classifyEndpointScope(pc.BaseURL)
		check(fmt.Sprintf("endpoint scope (%s)", scope), scope != endpointScopeRemote && scope != endpointScopeInvalid, detail)

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
			info("  Models available: %d", len(models))
		}

		// Check configured role models.
		for role, roleCfg := range llmCfg.Roles {
			if roleCfg.Provider != name {
				continue
			}
			if roleCfg.Model == "" {
				terminalOut("  ⚠ role %q: no model configured", role)
				continue
			}
			check(fmt.Sprintf("role %q model %q", role, roleCfg.Model),
				modelIDs[roleCfg.Model],
				"model not found in model list")
		}
	}

	if !flags.quiet {
		info("")
	}
	if issues > 0 {
		return fmt.Errorf("llm doctor: %d issue(s) found", issues)
	}
	if !flags.quiet {
		info("All LLM checks passed.")
	}
	return nil
}

type endpointScope string

const (
	endpointScopeInvalid      endpointScope = "invalid"
	endpointScopeLoopback     endpointScope = "loopback"
	endpointScopeLocalNetwork endpointScope = "local network"
	endpointScopeRemote       endpointScope = "remote"
)

var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

func classifyEndpointScope(baseURL string) (endpointScope, string) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return endpointScopeInvalid, fmt.Sprintf("invalid base_url: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return endpointScopeInvalid, "base_url must use http or https"
	}
	host := u.Hostname()
	if host == "" {
		return endpointScopeInvalid, "base_url must include a host"
	}
	if strings.EqualFold(host, "localhost") {
		return endpointScopeLoopback, ""
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return endpointScopeRemote, "remote hostname; use a loopback or local-network IP unless you intend to send manuscript excerpts off-device"
	}
	addr = addr.Unmap()
	switch {
	case addr.IsLoopback():
		return endpointScopeLoopback, ""
	case addr.IsPrivate(), addr.IsLinkLocalUnicast(), sharedAddressSpace.Contains(addr):
		return endpointScopeLocalNetwork, ""
	default:
		return endpointScopeRemote, "remote endpoint; use a loopback or local-network IP unless you intend to send manuscript excerpts off-device"
	}
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
