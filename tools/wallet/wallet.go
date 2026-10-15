// Package wallet provides the agent's Solana wallet tool.
package wallet

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/melodyogonna/solai/config"
	"github.com/melodyogonna/solai/llm"
	"github.com/melodyogonna/solai/tool"
	solwallet "github.com/melodyogonna/solai/wallet"
)

func init() {
	tool.Register("wallet",
		"The agent's Solana wallet: public address and message signing.",
		func() llm.Tool {
			kp, err := solwallet.Resolve(config.FromEnv().WalletSeed)
			if err != nil {
				return &tool.ErrorTool{ToolName: "wallet", Err: err}
			}
			return &walletTool{kp: &kp}
		})
}

type walletTool struct{ kp *solwallet.SolKeyPair }

func (t *walletTool) Name() string { return "wallet" }

func (t *walletTool) Description() string {
	return fmt.Sprintf("The agent's Solana wallet. Public address: %s. Actions:\n"+
		"  {\"action\":\"address\"} — return the base58 public key\n"+
		"  {\"action\":\"sign\",\"input\":\"<base64-encoded bytes>\"} — return a base64-encoded ed25519 signature",
		t.kp.Base58PubKey())
}

func (t *walletTool) Parameters() map[string]any { return llm.OpenObjectSchema() }

func (t *walletTool) Call(_ context.Context, argsJSON string) (string, error) {
	var req struct {
		Action string `json:"action"`
		Input  string `json:"input"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &req); err != nil || req.Action == "" {
		return t.kp.Base58PubKey(), nil
	}

	switch req.Action {
	case "address":
		return t.kp.Base58PubKey(), nil
	case "sign":
		msg, err := base64.StdEncoding.DecodeString(req.Input)
		if err != nil {
			return "", fmt.Errorf("decoding sign input: %w", err)
		}
		sig := ed25519.Sign(ed25519.PrivateKey(t.kp.PrivateKeyBytes()), msg)
		return base64.StdEncoding.EncodeToString(sig), nil
	default:
		return "", fmt.Errorf("unknown wallet action %q (supported: address, sign)", req.Action)
	}
}
