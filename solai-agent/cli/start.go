package cli

import (
	"fmt"

	"github.com/melodyogonna/solai/solai-agent/capability"
	solaiconfig "github.com/melodyogonna/solai/solai-agent/config"
	"github.com/melodyogonna/solai/solai-agent/session"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Run a session locally on this host",
	Long: `Start runs one agent session directly on this host.

The goal comes from "user-goals", the model from "model.name", and all
installed tools and built-in capabilities are available. For the control-plane
API (multiple sessions, event streaming), run "solaid" instead.

Prerequisites:
  solai config set model.name <openrouter-model-slug>
  solai config set provider.openrouter <api-key>
  solai config set user-goals "Monitor SOL price and report daily"`,
	Args: cobra.NoArgs,
	RunE: runStart,
}

func init() {
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := solaiconfig.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.Model.Name == "" {
		return fmt.Errorf("model not configured; run:\n  solai config set model.name <openrouter-model-slug>")
	}
	if cfg.Providers["openrouter"] == "" {
		return fmt.Errorf("no OpenRouter API key; run: solai config set provider.openrouter <key>")
	}
	if cfg.UserGoals == "" {
		return fmt.Errorf("user_goals is not configured; run: solai config set user-goals \"<goals>\"")
	}

	spec := session.Spec{
		Goals:        []string{cfg.UserGoals},
		Model:        cfg.Model.Name,
		Capabilities: capability.BuiltinNames(),
	}

	return runSession(cmd.Context(), cfg, spec, solaiconfig.ToolsDir(), logEmitter)
}
