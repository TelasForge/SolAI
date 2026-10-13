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
	"github.com/melodyogonna/solai/solai-agent/capability"
	solaiconfig "github.com/melodyogonna/solai/solai-agent/config"
	"github.com/melodyogonna/solai/solai-agent/prompts"
	"github.com/melodyogonna/solai/solai-agent/session"
	"github.com/melodyogonna/solai/solai-agent/tool"
	"github.com/melodyogonna/solai/solai-agent/wallet"
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

// runSession builds and runs one session to completion. It is shared by
// `solai start` (local run) and `solai __runtime` (control-plane-launched).
// Events are delivered to emit.
func runSession(ctx context.Context, cfg *solaiconfig.SolaiConfig, spec session.Spec, toolsDir string, emit session.Emitter) error {
	initLogger()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if spec.SystemPrompt == "" {
		spec.SystemPrompt = prompts.SystemPrompt
	}
	model := spec.Model
	if model == "" {
		model = cfg.Model.Name
	}
	client, err := newLLMForModel(cfg, model)
	if err != nil {
		return fmt.Errorf("initializing LLM: %w", err)
	}

	kp, err := wallet.CreateWallet(cfg.WalletSeed)
	if err != nil {
		return fmt.Errorf("creating wallet: %w", err)
	}
	registerDefaultCapabilities(&kp, cfg)
	capManager := capability.SetUp(spec.Capabilities)

	llmProvider := capability.NewLLMProviderFromMap(cfg.Providers, cfg.OpenRouterBaseURL)
	allTools, warnings, err := tool.LoadTools(toolsDir, llmProvider, capManager, cfg)
	if err != nil {
		return fmt.Errorf("loading tools: %w", err)
	}
	for _, w := range warnings {
		slog.Warn("tool setup warning", "err", w)
	}

	// The coordinator can call the selected capabilities directly, plus the
	// selected agentic tools.
	agentTools := append(capManager.GetAgentTools(), tool.Select(allTools, spec.Tools)...)

	slog.Info("session starting", "model", model, "goals", len(spec.Goals),
		"tools", len(agentTools), "wallet", kp.Base58PubKey())

	rt := session.New(spec, client, agentTools, emit)
	return rt.Run(ctx)
}

// newLLM builds an OpenRouter client for the config's selected model.
func newLLM(cfg *solaiconfig.SolaiConfig) (*llm.Client, error) {
	return newLLMForModel(cfg, cfg.Model.Name)
}

// newLLMForModel builds an OpenRouter client for a specific model slug.
func newLLMForModel(cfg *solaiconfig.SolaiConfig, model string) (*llm.Client, error) {
	return llm.NewClient(llm.Config{
		APIKey:  cfg.Providers["openrouter"],
		BaseURL: cfg.OpenRouterBaseURL,
		Model:   model,
		Referer: "https://github.com/melodyogonna/solai",
		Title:   "SolAI",
	})
}

// registerDefaultCapabilities registers the built-in capability factories. The
// specific set a session may use is chosen later via capability.SetUp.
func registerDefaultCapabilities(kp *wallet.SolKeyPair, cfg *solaiconfig.SolaiConfig) {
	capability.Register("communication", func() capability.Capability {
		return capability.NewCommunicationCapability()
	})
	capability.Register("wallet", func() capability.Capability {
		return capability.NewWalletCapability(kp)
	})
	capability.Register("solana", func() capability.Capability {
		return capability.NewSolanaCapability(kp, cfg.Solana.RPCURL, cfg.Solana.Commitment)
	})
	capability.Register("network-manager", func() capability.Capability {
		return capability.NewNetworkManagerCapability()
	})
	capability.Register("memory", func() capability.Capability {
		return capability.NewMemoryCapability()
	})
}

// logEmitter renders session events as structured logs for `solai start`.
func logEmitter(e session.Event) {
	switch e.Type {
	case session.EventGoalStarted:
		slog.Info("goal started", "goal", e.Message)
	case session.EventGoalDone:
		slog.Info("goal finished", "status", e.Status, "note", e.Message)
	case session.EventMessage:
		slog.Info("agent", "message", e.Message)
	case session.EventToolCall:
		slog.Info("tool call", "tool", e.Tool, "args", e.Args)
	case session.EventToolResult:
		slog.Debug("tool result", "tool", e.Tool, "result", e.Result)
	case session.EventError:
		slog.Error("session error", "message", e.Message)
	case session.EventSessionDone:
		slog.Info("session finished", "status", e.Status)
	}
}
