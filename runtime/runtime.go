// Package runtime wires an environment configuration and a session spec into a
// runnable session.
//
// It is the sandbox-unaware core of the runtime binary: tools are compiled in
// and called in-process, so it performs no isolation itself.
package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/melodyogonna/solai/config"
	"github.com/melodyogonna/solai/llm"
	"github.com/melodyogonna/solai/mcp"
	"github.com/melodyogonna/solai/prompts"
	"github.com/melodyogonna/solai/session"
	"github.com/melodyogonna/solai/tool"

	// Register the built-in tools.
	_ "github.com/melodyogonna/solai/tools"
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

	tools := tool.SetUp(spec.Tools)

	if len(spec.MCPServers) > 0 {
		mcpClient, err := mcp.Connect(ctx, toMCPServers(spec.MCPServers))
		if err != nil {
			return fmt.Errorf("connecting MCP servers: %w", err)
		}
		defer mcpClient.Close()
		tools = append(tools, mcpClient.Tools()...)
		slog.Info("mcp tools loaded", "servers", len(spec.MCPServers), "tools", len(mcpClient.Tools()))
	}

	slog.Info("session starting", "model", cfg.Model, "goals", len(spec.Goals),
		"tools", len(tools))

	return session.New(spec, client, tools, emit).Run(ctx)
}

func toMCPServers(servers []session.MCPServer) []mcp.Server {
	out := make([]mcp.Server, len(servers))
	for i, s := range servers {
		out[i] = mcp.Server{Name: s.Name, URL: s.URL, Headers: s.Headers}
	}
	return out
}
