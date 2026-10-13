package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/melodyogonna/solai/llm"
	"github.com/melodyogonna/solai/solai-agent/agent"
	"github.com/melodyogonna/solai/solai-agent/capability"
	solaiconfig "github.com/melodyogonna/solai/solai-agent/config"
	"github.com/melodyogonna/solai/solai-agent/prompts"
)

// initLogger configures the global slog logger. The level is controlled by the
// SOLAI_LOG_LEVEL env var (debug, info, warn, error; default: info).
func initLogger() {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("SOLAI_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

// agentRun initializes and runs the autonomous agent loop.
func agentRun(ctx context.Context, cfg *solaiconfig.SolaiConfig, toolsDir string) error {
	initLogger()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	llmClient, err := newLLM(cfg)
	if err != nil {
		return fmt.Errorf("initializing LLM: %w", err)
	}

	agentCfg, err := agent.LoadConfigFrom(cfg, toolsDir, prompts.SystemPrompt)
	if err != nil {
		return fmt.Errorf("building agent config: %w", err)
	}
	agentCfg.LLM = llmClient

	capability.Register("communication", func() capability.Capability {
		return capability.NewCommunicationCapability()
	})
	capability.Register("wallet", func() capability.Capability {
		return capability.NewWalletCapability(agentCfg.Wallet)
	})
	capability.Register("solana", func() capability.Capability {
		return capability.NewSolanaCapability(agentCfg.Wallet, cfg.Solana.RPCURL, cfg.Solana.Commitment)
	})
	capability.Register("network-manager", func() capability.Capability {
		return capability.NewNetworkManagerCapability()
	})
	capability.Register("memory", func() capability.Capability {
		return capability.NewMemoryCapability()
	})
	capManager := capability.SetUp([]string{"communication", "wallet", "solana", "network-manager", "memory"})

	slog.Info("SolAI agent starting",
		"model", cfg.Model.Name,
		"toolsDir", toolsDir,
		"cycleTimeout", agentCfg.CycleTimeout,
		"wallet", agentCfg.Wallet.Base58PubKey(),
	)

	agent.Run(ctx, agentCfg, capManager)
	slog.Info("SolAI agent stopped")
	return nil
}

// newLLM builds an OpenRouter client from the model config in cfg.
func newLLM(cfg *solaiconfig.SolaiConfig) (*llm.Client, error) {
	return llm.NewClient(llm.Config{
		APIKey:  cfg.Providers["openrouter"],
		BaseURL: cfg.OpenRouterBaseURL,
		Model:   cfg.Model.Name,
		Referer: "https://github.com/melodyogonna/solai",
		Title:   "SolAI",
	})
}
