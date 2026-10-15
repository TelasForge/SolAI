// Package config resolves the runtime configuration from the process
// environment.
//
// There is no config file: the surrounding platform (for example a container
// orchestrator, which injects environment variables into the container)
// constructs the environment the runtime runs in. This package simply reads
// what was injected.
package config

import (
	"fmt"
	"os"
)

// Environment variables the runtime reads.
const (
	// EnvModel is the OpenRouter model slug (e.g. "anthropic/claude-sonnet-4").
	EnvModel = "SOLAI_MODEL"

	// EnvOpenRouterKey is the OpenRouter API key.
	EnvOpenRouterKey = "OPENROUTER_API_KEY"

	// EnvOpenRouterBase overrides the OpenRouter API root.
	EnvOpenRouterBase = "SOLAI_OPENROUTER_BASE_URL"

	// EnvWalletSeed is the BIP39 mnemonic for the agent wallet. When unset a
	// new wallet is generated.
	EnvWalletSeed = "SOLAI_WALLET_SEED"

	// EnvRPCURL is the Solana RPC endpoint.
	EnvRPCURL = "SOLAI_RPC_URL"

	// EnvCommitment is the Solana commitment level.
	EnvCommitment = "SOLAI_COMMITMENT"
)

// Defaults for optional settings.
const (
	DefaultRPCURL     = "https://api.mainnet-beta.solana.com"
	DefaultCommitment = "confirmed"
)

// Config is the runtime configuration assembled from the environment.
type Config struct {
	Model             string
	OpenRouterAPIKey  string
	OpenRouterBaseURL string
	WalletSeed        string
	RPCURL            string
	Commitment        string
}

// FromEnv reads configuration from the process environment.
func FromEnv() Config {
	return Config{
		Model:             os.Getenv(EnvModel),
		OpenRouterAPIKey:  os.Getenv(EnvOpenRouterKey),
		OpenRouterBaseURL: os.Getenv(EnvOpenRouterBase),
		WalletSeed:        os.Getenv(EnvWalletSeed),
		RPCURL:            envOr(EnvRPCURL, DefaultRPCURL),
		Commitment:        envOr(EnvCommitment, DefaultCommitment),
	}
}

// Validate reports the first required setting that is missing.
func (c Config) Validate() error {
	if c.Model == "" {
		return fmt.Errorf("%s is not set (OpenRouter model slug)", EnvModel)
	}
	if c.OpenRouterAPIKey == "" {
		return fmt.Errorf("%s is not set (OpenRouter API key)", EnvOpenRouterKey)
	}
	switch c.Commitment {
	case "finalized", "confirmed", "processed":
	default:
		return fmt.Errorf("%s must be finalized, confirmed, or processed, got %q", EnvCommitment, c.Commitment)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
