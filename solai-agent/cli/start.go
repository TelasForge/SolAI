package cli

import (
	"fmt"

	solaiconfig "github.com/melodyogonna/solai/solai-agent/config"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the autonomous agent",
	Long: `Start launches the SolAI agent.

Prerequisites:
  solai config set model.name <openrouter-model-slug>   (e.g. anthropic/claude-sonnet-4)
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

	return agentRun(cmd.Context(), cfg, solaiconfig.ToolsDir())
}
