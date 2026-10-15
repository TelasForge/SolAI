package config

import (
	"strings"
	"testing"
)

func TestFromEnv_ReadsAllVariables(t *testing.T) {
	t.Setenv(EnvModel, "anthropic/claude-sonnet-4")
	t.Setenv(EnvOpenRouterKey, "sk-or-key")
	t.Setenv(EnvOpenRouterBase, "https://example.test/api/v1")
	t.Setenv(EnvWalletSeed, "word1 word2")
	t.Setenv(EnvRPCURL, "https://rpc.example.test")
	t.Setenv(EnvCommitment, "finalized")
	t.Setenv(EnvToolsDir, "/opt/tools")

	cfg := FromEnv()
	if cfg.Model != "anthropic/claude-sonnet-4" {
		t.Errorf("Model: got %q", cfg.Model)
	}
	if cfg.OpenRouterAPIKey != "sk-or-key" {
		t.Errorf("OpenRouterAPIKey: got %q", cfg.OpenRouterAPIKey)
	}
	if cfg.OpenRouterBaseURL != "https://example.test/api/v1" {
		t.Errorf("OpenRouterBaseURL: got %q", cfg.OpenRouterBaseURL)
	}
	if cfg.WalletSeed != "word1 word2" {
		t.Errorf("WalletSeed: got %q", cfg.WalletSeed)
	}
	if cfg.RPCURL != "https://rpc.example.test" {
		t.Errorf("RPCURL: got %q", cfg.RPCURL)
	}
	if cfg.Commitment != "finalized" {
		t.Errorf("Commitment: got %q", cfg.Commitment)
	}
	if cfg.ToolsDir != "/opt/tools" {
		t.Errorf("ToolsDir: got %q", cfg.ToolsDir)
	}
}

func TestFromEnv_Defaults(t *testing.T) {
	for _, key := range []string{EnvRPCURL, EnvCommitment, EnvToolsDir} {
		t.Setenv(key, "")
	}
	cfg := FromEnv()
	if cfg.RPCURL != DefaultRPCURL {
		t.Errorf("RPCURL: got %q, want %q", cfg.RPCURL, DefaultRPCURL)
	}
	if cfg.Commitment != DefaultCommitment {
		t.Errorf("Commitment: got %q, want %q", cfg.Commitment, DefaultCommitment)
	}
	if cfg.ToolsDir == "" {
		t.Error("ToolsDir: expected a fallback")
	}
}

func TestValidate(t *testing.T) {
	base := Config{Model: "m", OpenRouterAPIKey: "k", Commitment: "confirmed"}

	if err := base.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"missing model", Config{OpenRouterAPIKey: "k", Commitment: "confirmed"}, EnvModel},
		{"missing key", Config{Model: "m", Commitment: "confirmed"}, EnvOpenRouterKey},
		{"bad commitment", Config{Model: "m", OpenRouterAPIKey: "k", Commitment: "bogus"}, EnvCommitment},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want error mentioning %q", err, tc.want)
			}
		})
	}
}
