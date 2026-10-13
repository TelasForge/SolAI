package cli

import (
	"encoding/json"
	"fmt"
	"os"

	solaiconfig "github.com/melodyogonna/solai/solai-agent/config"
	"github.com/melodyogonna/solai/solai-agent/session"
	"github.com/spf13/cobra"
)

// runtimeCmd is the hidden entrypoint the control plane uses to run a session.
// It reads a session.Spec from stdin and streams events as JSON lines on stdout.
var runtimeCmd = &cobra.Command{
	Use:    "__runtime",
	Hidden: true,
	Short:  "Run a session runtime (invoked by the control plane)",
	Args:   cobra.NoArgs,
	RunE:   runRuntime,
}

func init() {
	rootCmd.AddCommand(runtimeCmd)
}

func runRuntime(cmd *cobra.Command, args []string) error {
	var spec session.Spec
	if err := json.NewDecoder(os.Stdin).Decode(&spec); err != nil {
		return fmt.Errorf("reading session spec: %w", err)
	}
	if len(spec.Goals) == 0 {
		return fmt.Errorf("session spec has no goals")
	}

	cfg, err := solaiconfig.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	emit := session.NewLineEmitter(os.Stdout)
	return runSession(cmd.Context(), cfg, spec, solaiconfig.ToolsDir(), emit)
}
