// Package runtime wires an environment configuration and a session spec into a
// runnable session.
//
// It is the sandbox-unaware core of the runtime binary: it launches tools as
// commands and never performs isolation itself.
package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/melodyogonna/solai/capability"
	"github.com/melodyogonna/solai/config"
	"github.com/melodyogonna/solai/llm"
	"github.com/melodyogonna/solai/prompts"
	"github.com/melodyogonna/solai/session"
	"github.com/melodyogonna/solai/tool"
	"github.com/melodyogonna/solai/wallet"
)

// Run builds and runs one session to completion, emitting events to emit.
//
// The model is taken from the session spec when set, otherwise from the
// environment configuration.
func Run(ctx context.Context, cfg config.Config, spec session.Spec, emit session.Emitter) error {
	if spec.Model != "" {
		cfg.Model = spec.Model
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if spec.SystemPrompt == "" {
		spec.SystemPrompt = prompts.SystemPrompt
	}

	client, err := llm.NewClient(llm.Config{
		APIKey:  cfg.OpenRouterAPIKey,
		BaseURL: cfg.OpenRouterBaseURL,
		Model:   cfg.Model,
		Referer: "https://github.com/melodyogonna/solai",
		Title:   "SolAI",
	})
	if err != nil {
		return fmt.Errorf("initializing LLM: %w", err)
	}

	kp, err := wallet.CreateWallet(cfg.WalletSeed)
	if err != nil {
		return fmt.Errorf("creating wallet: %w", err)
	}
	registerCapabilities(&kp, cfg)
	capManager := capability.SetUp(spec.Capabilities)

	provider := capability.NewLLMProvider(cfg.OpenRouterAPIKey, cfg.OpenRouterBaseURL)
	allTools, warnings, err := tool.LoadTools(cfg.ToolsDir, provider, capManager)
	if err != nil {
		return fmt.Errorf("loading tools: %w", err)
	}
	for _, w := range warnings {
		slog.Warn("tool setup warning", "err", w)
	}

	// The coordinator can call the selected capabilities directly, plus the
	// selected agentic tools.
	agentTools := append(capManager.GetAgentTools(), tool.Select(allTools, spec.Tools)...)

	slog.Info("session starting", "model", cfg.Model, "goals", len(spec.Goals),
		"tools", len(agentTools), "wallet", kp.Base58PubKey())

	return session.New(spec, client, agentTools, emit).Run(ctx)
}

// registerCapabilities registers the built-in capability factories. The set a
// session may actually use is chosen via capability.SetUp.
func registerCapabilities(kp *wallet.SolKeyPair, cfg config.Config) {
	capability.Register("communication", func() capability.Capability {
		return capability.NewCommunicationCapability()
	})
	capability.Register("wallet", func() capability.Capability {
		return capability.NewWalletCapability(kp)
	})
	capability.Register("solana", func() capability.Capability {
		return capability.NewSolanaCapability(kp, cfg.RPCURL, cfg.Commitment)
	})
	capability.Register("network-manager", func() capability.Capability {
		return capability.NewNetworkManagerCapability()
	})
	capability.Register("memory", func() capability.Capability {
		return capability.NewMemoryCapability()
	})
}
