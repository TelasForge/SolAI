package capability

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/melodyogonna/solai/wallet"
)

// WalletCapability exposes the agent's Solana wallet as a Regular capability.
// The coordinator LLM can call it directly to retrieve the wallet address.
// Agentic tools can request signing via the capability request protocol.
type WalletCapability struct {
	keypair *wallet.SolKeyPair
}

// NewWalletCapability creates a WalletCapability wrapping the given keypair.
func NewWalletCapability(kp *wallet.SolKeyPair) *WalletCapability {
	return &WalletCapability{keypair: kp}
}

func (w *WalletCapability) Name() string { return "wallet" }

// Class returns Regular — the wallet is callable by the coordinator LLM and
// requestable by agentic tools (e.g. for transaction signing).
func (w *WalletCapability) Class() CapabilityClass { return Regular }

// Description is shown to the coordinator LLM as the tool description.
func (w *WalletCapability) Description() string {
	return fmt.Sprintf(
		"Your Solana wallet. Public address: %s. "+
			"Call this tool with any input to retrieve the wallet's public address. "+
			"Use this when you need to pass your wallet address to another tool or include it in a transaction.",
		w.keypair.Base58PubKey(),
	)
}

// ToolRequestDescription documents the actions agentic tools can request.
func (w *WalletCapability) ToolRequestDescription() string {
	return `Signs data and provides the wallet address on behalf of the coordinator.
Available actions:
- "sign":    input is base64-encoded bytes to sign → response.output is base64-encoded ed25519 signature
- "address": input is ignored → response.output is the base58-encoded Solana public key`
}

// Execute handles both direct LLM calls and capability request dispatches.
//
// When called by the coordinator LLM (plain string input), it returns the
// wallet's public key. When dispatched via the request protocol, input is a
// JSON object with "action" and "input" fields.
func (w *WalletCapability) Execute(_ context.Context, input string) (string, error) {
	var req struct {
		Action string `json:"action"`
		Input  string `json:"input"`
	}
	if err := json.Unmarshal([]byte(input), &req); err != nil || req.Action == "" {
		slog.Debug("capability called", "capability", "wallet", "action", "address")
		return w.keypair.Base58PubKey(), nil
	}

	slog.Debug("capability called", "capability", "wallet", "action", req.Action)

	switch req.Action {
	case "address":
		return w.keypair.Base58PubKey(), nil

	case "sign":
		msgBytes, err := base64.StdEncoding.DecodeString(req.Input)
		if err != nil {
			return "", fmt.Errorf("decoding sign input: %w", err)
		}
		sig := ed25519.Sign(ed25519.PrivateKey(w.keypair.PrivateKeyBytes()), msgBytes)
		return base64.StdEncoding.EncodeToString(sig), nil

	default:
		return "", fmt.Errorf("unknown wallet action %q (supported: address, sign)", req.Action)
	}
}
