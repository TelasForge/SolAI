package cli

import (
	"testing"

	solaiconfig "github.com/melodyogonna/solai/solai-agent/config"
)

// ---- newLLM ----------------------------------------------------------------

func TestNewLLM_BuildsOpenRouterClient(t *testing.T) {
	cfg := solaiconfig.DefaultConfig()
	cfg.Model.Name = "anthropic/claude-sonnet-4"
	cfg.Providers["openrouter"] = "sk-or-test"

	client, err := newLLM(cfg)
	if err != nil {
		t.Fatalf("newLLM: %v", err)
	}
	if client.Model() != "anthropic/claude-sonnet-4" {
		t.Errorf("model: got %q", client.Model())
	}
}

func TestNewLLM_MissingKey_Error(t *testing.T) {
	cfg := solaiconfig.DefaultConfig()
	cfg.Model.Name = "anthropic/claude-sonnet-4"
	// no openrouter key
	if _, err := newLLM(cfg); err == nil {
		t.Fatal("expected error when OpenRouter key is missing")
	}
}
