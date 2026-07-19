package provider

import (
	"fmt"

	"github.com/nusapuksic/story/internal/config"
)

// ErrNoProvider is returned when no provider is configured for the requested
// role.
var ErrNoProvider = fmt.Errorf("no LLM provider configured")

// ForRole constructs a Provider for a named role (extraction, verification,
// discussion) from project configuration.  It returns ErrNoProvider when the
// role or its provider is not configured.
func ForRole(cfg config.LLMConfig, role string) (Provider, string, error) {
	roleCfg, ok := cfg.Roles[role]
	if !ok || roleCfg.Provider == "" {
		// Fall back to the default provider with an empty model.
		if cfg.DefaultProvider == "" {
			return nil, "", ErrNoProvider
		}
		roleCfg = config.LLMRoleConfig{Provider: cfg.DefaultProvider}
	}
	p, err := buildProvider(cfg, roleCfg.Provider)
	if err != nil {
		return nil, "", err
	}
	return p, roleCfg.Model, nil
}

// buildProvider constructs a Provider from a named provider entry in cfg.
func buildProvider(cfg config.LLMConfig, name string) (Provider, error) {
	pc, ok := cfg.Providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found in configuration", name)
	}
	switch pc.Type {
	case "openai-compatible", "":
		return NewOpenAI(pc.BaseURL, pc.APIKeyEnv, pc.RequestTimeoutSeconds), nil
	default:
		return nil, fmt.Errorf("unsupported provider type %q", pc.Type)
	}
}
