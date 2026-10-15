// Package solana provides the agent's Solana RPC tool.
package solana

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	bin "github.com/gagliardetto/binary"
	sol "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/melodyogonna/solai/config"
	"github.com/melodyogonna/solai/llm"
	"github.com/melodyogonna/solai/tool"
	solwallet "github.com/melodyogonna/solai/wallet"
)

func init() {
	tool.Register("solana",
		"Solana RPC: balance, transfer, blockhash, send_transaction, account info.",
		func() llm.Tool {
			cfg := config.FromEnv()
			kp, err := solwallet.Resolve(cfg.WalletSeed)
			if err != nil {
				return &tool.ErrorTool{ToolName: "solana", Err: err}
			}
			return &solanaTool{
				kp:         &kp,
				rpcURL:     cfg.RPCURL,
				commitment: commitmentType(cfg.Commitment),
				client:     rpc.New(cfg.RPCURL),
			}
		})
}

type solanaTool struct {
	kp         *solwallet.SolKeyPair
	rpcURL     string
	commitment rpc.CommitmentType
	client     *rpc.Client
}

func commitmentType(s string) rpc.CommitmentType {
	switch s {
	case "finalized":
		return rpc.CommitmentFinalized
	case "processed":
		return rpc.CommitmentProcessed
	default:
		return rpc.CommitmentConfirmed
	}
}

func (t *solanaTool) Name() string { return "solana" }

func (t *solanaTool) Description() string {
	return fmt.Sprintf("Interact with the Solana blockchain. RPC: %s (%s). Wallet: %s. Actions:\n"+
		"  {\"action\":\"get_balance\"} — SOL balance\n"+
		"  {\"action\":\"transfer_sol\",\"to\":\"<base58>\",\"lamports\":<N>} — transfer SOL\n"+
		"  {\"action\":\"get_recent_blockhash\"}\n"+
		"  {\"action\":\"send_transaction\",\"transaction\":\"<base64>\"} — sign and submit a pre-built transaction\n"+
		"  {\"action\":\"get_account_info\",\"address\":\"<base58>\"}",
		t.rpcURL, t.commitment, t.kp.Base58PubKey())
}

func (t *solanaTool) Parameters() map[string]any { return llm.OpenObjectSchema() }

func (t *solanaTool) Call(ctx context.Context, argsJSON string) (string, error) {
	var req struct {
		Action      string `json:"action"`
		Address     string `json:"address"`
		To          string `json:"to"`
		Lamports    uint64 `json:"lamports"`
		Transaction string `json:"transaction"`
		Input       string `json:"input"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &req); err != nil {
		return "", fmt.Errorf("solana: invalid input JSON: %w", err)
	}

	switch req.Action {
	case "get_balance":
		return t.getBalance(ctx)
	case "transfer_sol":
		return t.transferSOL(ctx, req.To, req.Lamports)
	case "get_recent_blockhash":
		return t.getRecentBlockhash(ctx)
	case "send_transaction":
		tx := req.Transaction
		if tx == "" {
			tx = req.Input
		}
		return t.sendTransaction(ctx, tx)
	case "get_account_info":
		return t.getAccountInfo(ctx, req.Address)
	default:
		return "", fmt.Errorf("solana: unknown action %q; supported: get_balance, transfer_sol, get_recent_blockhash, send_transaction, get_account_info", req.Action)
	}
}

func (t *solanaTool) getBalance(ctx context.Context) (string, error) {
	pub, err := sol.PublicKeyFromBase58(t.kp.Base58PubKey())
	if err != nil {
		return "", fmt.Errorf("solana: invalid public key: %w", err)
	}
	result, err := t.client.GetBalance(ctx, pub, t.commitment)
	if err != nil {
		return "", fmt.Errorf("solana: GetBalance RPC: %w", err)
	}
	return marshal(map[string]any{
		"lamports": result.Value,
		"sol":      float64(result.Value) / 1e9,
	}), nil
}

func (t *solanaTool) getRecentBlockhash(ctx context.Context) (string, error) {
	result, err := t.client.GetLatestBlockhash(ctx, t.commitment)
	if err != nil {
		return "", fmt.Errorf("solana: GetLatestBlockhash RPC: %w", err)
	}
	return marshal(map[string]any{
		"blockhash":               result.Value.Blockhash.String(),
		"last_valid_block_height": result.Value.LastValidBlockHeight,
	}), nil
}

func (t *solanaTool) transferSOL(ctx context.Context, toAddr string, lamports uint64) (string, error) {
	if toAddr == "" {
		return "", fmt.Errorf("solana: transfer_sol requires 'to'")
	}
	if lamports == 0 {
		return "", fmt.Errorf("solana: transfer_sol requires lamports > 0")
	}

	from, err := sol.PublicKeyFromBase58(t.kp.Base58PubKey())
	if err != nil {
		return "", fmt.Errorf("solana: invalid source public key: %w", err)
	}
	to, err := sol.PublicKeyFromBase58(toAddr)
	if err != nil {
		return "", fmt.Errorf("solana: invalid destination address %q: %w", toAddr, err)
	}
	recent, err := t.client.GetLatestBlockhash(ctx, t.commitment)
	if err != nil {
		return "", fmt.Errorf("solana: GetLatestBlockhash RPC: %w", err)
	}

	tx, err := sol.NewTransaction(
		[]sol.Instruction{system.NewTransferInstruction(lamports, from, to).Build()},
		recent.Value.Blockhash,
		sol.TransactionPayer(from),
	)
	if err != nil {
		return "", fmt.Errorf("solana: building transaction: %w", err)
	}
	if err := t.signTx(tx, from); err != nil {
		return "", fmt.Errorf("solana: signing transaction: %w", err)
	}

	sig, err := t.client.SendTransaction(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("solana: SendTransaction RPC: %w", err)
	}
	return marshal(map[string]any{"signature": sig.String(), "lamports": lamports, "to": toAddr}), nil
}

func (t *solanaTool) sendTransaction(ctx context.Context, txBase64 string) (string, error) {
	if txBase64 == "" {
		return "", fmt.Errorf("solana: send_transaction requires 'transaction' (base64)")
	}
	txBytes, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		return "", fmt.Errorf("solana: decoding transaction base64: %w", err)
	}
	tx, err := sol.TransactionFromDecoder(bin.NewBinDecoder(txBytes))
	if err != nil {
		return "", fmt.Errorf("solana: deserializing transaction: %w", err)
	}
	from, err := sol.PublicKeyFromBase58(t.kp.Base58PubKey())
	if err != nil {
		return "", fmt.Errorf("solana: invalid public key: %w", err)
	}
	if err := t.signTx(tx, from); err != nil {
		return "", fmt.Errorf("solana: signing transaction: %w", err)
	}
	sig, err := t.client.SendTransaction(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("solana: SendTransaction RPC: %w", err)
	}
	return marshal(map[string]any{"signature": sig.String()}), nil
}

// signTx signs for the wallet key, leaving other signers' slots untouched so
// pre-signed transactions (e.g. from Jupiter) keep their signatures.
func (t *solanaTool) signTx(tx *sol.Transaction, signer sol.PublicKey) error {
	priv := sol.PrivateKey(t.kp.PrivateKeyBytes())
	_, err := tx.Sign(func(key sol.PublicKey) *sol.PrivateKey {
		if key.Equals(signer) {
			return &priv
		}
		return nil
	})
	return err
}

func (t *solanaTool) getAccountInfo(ctx context.Context, address string) (string, error) {
	if address == "" {
		return "", fmt.Errorf("solana: get_account_info requires 'address'")
	}
	pub, err := sol.PublicKeyFromBase58(address)
	if err != nil {
		return "", fmt.Errorf("solana: invalid address %q: %w", address, err)
	}
	result, err := t.client.GetAccountInfoWithOpts(ctx, pub, &rpc.GetAccountInfoOpts{
		Encoding:   sol.EncodingBase64,
		Commitment: t.commitment,
	})
	if err != nil {
		return "", fmt.Errorf("solana: GetAccountInfo RPC: %w", err)
	}
	if result == nil || result.Value == nil {
		return marshal(map[string]any{"exists": false, "address": address}), nil
	}
	acc := result.Value
	return marshal(map[string]any{
		"exists":     true,
		"address":    address,
		"owner":      acc.Owner.String(),
		"lamports":   acc.Lamports,
		"sol":        float64(acc.Lamports) / 1e9,
		"executable": acc.Executable,
		"rent_epoch": acc.RentEpoch,
		"data":       base64.StdEncoding.EncodeToString(acc.Data.GetBinary()),
	}), nil
}

func marshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
