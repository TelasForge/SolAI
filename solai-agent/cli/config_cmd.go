package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/melodyogonna/solai/solai-agent/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage SolAI configuration",
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Long: `Set a configuration value in ~/.solai/config.json.

Model (coordinator LLM):
  model.name           OpenRouter model slug (e.g. anthropic/claude-sonnet-4, openai/gpt-4o)

Provider credentials (used by coordinator and injected into agentic tools):
  provider.openrouter  OpenRouter API key

Agent settings:
  openrouter.base-url  Override the OpenRouter API root (optional)
  user-goals           Goals the agent should pursue autonomously
  cycle-timeout        Duration between agent cycles (e.g. 5m, 1h)
  wallet-seed          BIP39 seed phrase for the agent wallet

Solana settings:
  solana.rpc-url       Solana RPC endpoint (default: mainnet-beta)
  solana.commitment    Commitment level: finalized, confirmed, or processed

Tool environment variables:
  tool-env.<tool>.<VAR>  Set an env var for a specific tool (e.g. tool-env.birdeye.BIRDEYE_API_KEY)`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a configuration value",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigGet,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration values (sensitive values are redacted)",
	Args:  cobra.NoArgs,
	RunE:  runConfigList,
}

func init() {
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configListCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := cfg.Set(key, value); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Printf("Set %s\n", key)
	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	val, err := cfg.Get(key)
	if err != nil {
		return err
	}
	fmt.Println(val)
	return nil
}

func runConfigList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	providers := make(map[string]string)
	for k, v := range cfg.Providers {
		providers[k] = redact(v)
	}

	toolEnv := make(map[string]map[string]string)
	for toolName, vars := range cfg.ToolEnv {
		redacted := make(map[string]string, len(vars))
		for k, v := range vars {
			redacted[k] = redact(v)
		}
		toolEnv[toolName] = redacted
	}

	display := map[string]any{
		"model": map[string]any{
			"name": cfg.Model.Name,
		},
		"providers":           providers,
		"openrouter_base_url": cfg.OpenRouterBaseURL,
		"user_goals":          cfg.UserGoals,
		"cycle_timeout":       cfg.CycleTimeout,
		"wallet_seed":         redact(cfg.WalletSeed),
		"solana": map[string]any{
			"rpc_url":    cfg.Solana.RPCURL,
			"commitment": cfg.Solana.Commitment,
		},
		"tool_env": toolEnv,
	}

	data, err := json.MarshalIndent(display, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// redact replaces a non-empty sensitive string with a redaction marker,
// preserving a short prefix for identification.
func redact(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > 8 {
		return s[:4] + strings.Repeat("*", len(s)-4)
	}
	return strings.Repeat("*", len(s))
}
