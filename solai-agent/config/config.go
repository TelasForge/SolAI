// Package config manages the ~/.solai/ configuration directory and config.json.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SolaiConfig is the top-level configuration stored in ~/.solai/config.json.
type SolaiConfig struct {
	// Model selects the LLM the coordinator uses, as an OpenRouter model slug
	// (e.g. "anthropic/claude-sonnet-4", "openai/gpt-4o", "google/gemini-2.5-pro").
	Model ModelConfig `json:"model"`

	// Providers holds API credentials keyed by provider name. OpenRouter is the
	// only provider used by this build; the key is "openrouter".
	Providers map[string]string `json:"providers"`

	// OpenRouterBaseURL overrides the OpenRouter API root. Empty uses the default.
	OpenRouterBaseURL string `json:"openrouter_base_url,omitempty"`

	WalletSeed   string                       `json:"wallet_seed"`
	CycleTimeout string                       `json:"cycle_timeout"`
	UserGoals    string                       `json:"user_goals"`
	Solana       SolanaConfig                 `json:"solana"`
	ToolEnv      map[string]map[string]string `json:"tool_env,omitempty"` // tool name → var name → value
}

// SolanaConfig controls how the agent interacts with the Solana blockchain.
type SolanaConfig struct {
	RPCURL     string `json:"rpc_url"`    // default: https://api.mainnet-beta.solana.com
	Commitment string `json:"commitment"` // "finalized", "confirmed", or "processed"
}

// ModelConfig identifies the LLM the coordinator uses.
type ModelConfig struct {
	// Name is an OpenRouter model slug, e.g. "anthropic/claude-sonnet-4".
	Name string `json:"name"`
}

// Dir returns the ~/.solai directory path.
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".solai")
	}
	return filepath.Join(home, ".solai")
}

// ToolsDir returns the ~/.solai/tools directory path.
func ToolsDir() string {
	path := filepath.Join(Dir(), "tools")
	_ = os.MkdirAll(path, 0o755)
	return path
}

// ConfigPath returns the path to ~/.solai/config.json.
func ConfigPath() string {
	return filepath.Join(Dir(), "config.json")
}

// DefaultConfig returns a SolaiConfig with sensible defaults.
func DefaultConfig() *SolaiConfig {
	return &SolaiConfig{
		Providers:    map[string]string{},
		CycleTimeout: "5m",
		Solana: SolanaConfig{
			RPCURL:     "https://api.mainnet-beta.solana.com",
			Commitment: "confirmed",
		},
	}
}

// Load reads ~/.solai/config.json, creating it with defaults if absent.
func Load() (*SolaiConfig, error) {
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := DefaultConfig()
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("creating default config: %w", err)
		}
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

// Save writes the config atomically (temp file + rename) to ~/.solai/config.json.
func (c *SolaiConfig) Save() error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	tmp := ConfigPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing temp config: %w", err)
	}
	if err := os.Rename(tmp, ConfigPath()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp config: %w", err)
	}
	return nil
}

// Set updates a single configuration value by dot-notation key.
//
// Supported keys: model.name, provider.<name>, openrouter.base-url,
// wallet-seed, cycle-timeout, user-goals,
// solana.rpc-url, solana.commitment, tool-env.<tool>.<VAR>.
func (c *SolaiConfig) Set(key, value string) error {
	switch key {
	case "model.name":
		c.Model.Name = value
	case "openrouter.base-url":
		c.OpenRouterBaseURL = value
	case "wallet-seed":
		c.WalletSeed = value
	case "cycle-timeout":
		c.CycleTimeout = value
	case "user-goals":
		c.UserGoals = value
	case "solana.rpc-url":
		c.Solana.RPCURL = value
	case "solana.commitment":
		switch value {
		case "finalized", "confirmed", "processed":
			c.Solana.Commitment = value
		default:
			return fmt.Errorf("solana.commitment: expected finalized/confirmed/processed, got %q", value)
		}
	default:
		if provider, ok := strings.CutPrefix(key, "provider."); ok {
			c.ensureProviders()
			c.Providers[provider] = value
			return nil
		}
		if rest, ok := strings.CutPrefix(key, "tool-env."); ok {
			dot := strings.IndexByte(rest, '.')
			if dot < 1 || dot == len(rest)-1 {
				return fmt.Errorf("tool-env key must be tool-env.<tool>.<VAR>, got %q", key)
			}
			toolName, varName := rest[:dot], rest[dot+1:]
			c.ensureToolEnv(toolName)
			c.ToolEnv[toolName][varName] = value
			return nil
		}
		return fmt.Errorf("unknown config key %q; valid keys: model.name, provider.<name>, openrouter.base-url, wallet-seed, cycle-timeout, user-goals, solana.rpc-url, solana.commitment, tool-env.<tool>.<VAR>", key)
	}
	return nil
}

// Get retrieves a single configuration value by dot-notation key.
func (c *SolaiConfig) Get(key string) (string, error) {
	switch key {
	case "model.name":
		return c.Model.Name, nil
	case "openrouter.base-url":
		return c.OpenRouterBaseURL, nil
	case "wallet-seed":
		return c.WalletSeed, nil
	case "cycle-timeout":
		return c.CycleTimeout, nil
	case "user-goals":
		return c.UserGoals, nil
	case "solana.rpc-url":
		return c.Solana.RPCURL, nil
	case "solana.commitment":
		return c.Solana.Commitment, nil
	default:
		if provider, ok := strings.CutPrefix(key, "provider."); ok {
			return c.Providers[provider], nil
		}
		if rest, ok := strings.CutPrefix(key, "tool-env."); ok {
			dot := strings.IndexByte(rest, '.')
			if dot < 1 || dot == len(rest)-1 {
				return "", fmt.Errorf("tool-env key must be tool-env.<tool>.<VAR>, got %q", key)
			}
			toolName, varName := rest[:dot], rest[dot+1:]
			if c.ToolEnv == nil {
				return "", nil
			}
			return c.ToolEnv[toolName][varName], nil
		}
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

// ToolEnvFor returns the configured environment variables for a tool as a
// "KEY=VALUE" slice ready for injection into a subprocess environment.
func (c *SolaiConfig) ToolEnvFor(toolName string) []string {
	if c.ToolEnv == nil {
		return nil
	}
	vars := c.ToolEnv[toolName]
	if len(vars) == 0 {
		return nil
	}
	result := make([]string, 0, len(vars))
	for k, v := range vars {
		result = append(result, k+"="+v)
	}
	return result
}

func (c *SolaiConfig) ensureProviders() {
	if c.Providers == nil {
		c.Providers = make(map[string]string)
	}
}

func (c *SolaiConfig) ensureToolEnv(toolName string) {
	if c.ToolEnv == nil {
		c.ToolEnv = make(map[string]map[string]string)
	}
	if c.ToolEnv[toolName] == nil {
		c.ToolEnv[toolName] = make(map[string]string)
	}
}
