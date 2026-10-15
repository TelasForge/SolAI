package capability

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/melodyogonna/solai/wallet"
)

const (
	defaultSolanaRPC  = "https://api.mainnet-beta.solana.com"
	defaultCommitment = "confirmed"
)

// SolanaCapability is a Regular capability that allows the agent to interact
// with the Solana blockchain: querying balances, fetching blockhashes, and
// submitting signed transactions.
type SolanaCapability struct {
	keypair    *wallet.SolKeyPair
	rpcURL     string
	commitment rpc.CommitmentType
	client     *rpc.Client
}

// NewSolanaCapability creates a SolanaCapability for the given keypair and RPC endpoint.
// Empty rpcURL or commitment fall back to mainnet and "confirmed" respectively.
func NewSolanaCapability(kp *wallet.SolKeyPair, rpcURL, commitment string) *SolanaCapability {
	if rpcURL == "" {
		rpcURL = defaultSolanaRPC
	}
	ct := rpc.CommitmentConfirmed
	switch commitment {
	case "finalized":
		ct = rpc.CommitmentFinalized
	case "processed":
		ct = rpc.CommitmentProcessed
	}
	return &SolanaCapability{
		keypair:    kp,
		rpcURL:     rpcURL,
		commitment: ct,
		client:     rpc.New(rpcURL),
	}
}

func (s *SolanaCapability) Name() string { return "solana" }

func (s *SolanaCapability) Class() CapabilityClass { return Regular }

// ToolRequestDescription describes the actions agentic tools can request from
// the coordinator via the capability request protocol.
func (s *SolanaCapability) ToolRequestDescription() string {
	return "Signs and submits Solana transactions on behalf of the agent wallet.\n" +
		"  action: send_transaction — adds the agent's signature to a pre-built transaction and submits it to the network\n" +
		"  input: base64-encoded unsigned Solana transaction (as returned by Jupiter /swap or similar)\n" +
		"  returns: {\"signature\":\"<base58_txid>\"}"
}

// Description is injected into each cycle prompt so the LLM knows how to use
// the Solana capability.
func (s *SolanaCapability) Description() string {
	return fmt.Sprintf(
		"You can interact with the Solana blockchain. RPC: %s (commitment: %s). "+
			"Your wallet: %s. "+
			"Call this capability with JSON:\n"+
			`  {"action":"get_balance"} — SOL balance in lamports`+"\n"+
			`  {"action":"transfer_sol","to":"<base58>","lamports":<N>} — transfer SOL`+"\n"+
			`  {"action":"get_recent_blockhash"} — latest blockhash`+"\n"+
			`  {"action":"send_transaction","transaction":"<base64>"} — sign and submit a pre-built transaction`+"\n"+
			"    Use send_transaction when a tool (e.g. Jupiter) returns a serialized transaction for you to sign.\n"+
			`  {"action":"get_account_info","address":"<base58>"} — fetch account owner, lamports, data (base64), and flags`,
		s.rpcURL, s.commitment, s.keypair.Base58PubKey(),
	)
}

// solanaInput is the JSON structure the LLM sends to Execute.
type solanaInput struct {
	Action      string `json:"action"`
	Address     string `json:"address,omitempty"` // base58 account address
	To          string `json:"to,omitempty"`
	Lamports    uint64 `json:"lamports,omitempty"`
	Transaction string `json:"transaction,omitempty"` // base64-encoded serialized transaction
	Input       string `json:"input,omitempty"`       // alias for Transaction; used in capability request dispatch
}

// Execute dispatches a Solana action based on the JSON input string.
func (s *SolanaCapability) Execute(ctx context.Context, input string) (string, error) {
	var req solanaInput
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("solana: invalid input JSON: %w", err)
	}
	slog.Debug("capability called", "capability", "solana", "action", req.Action)
	switch req.Action {
	case "get_balance":
		return s.getBalance(ctx)
	case "transfer_sol":
		return s.transferSOL(ctx, req.To, req.Lamports)
	case "get_recent_blockhash":
		return s.getRecentBlockhash(ctx)
	case "send_transaction":
		tx := req.Transaction
		if tx == "" {
			tx = req.Input
		}
		return s.sendTransaction(ctx, tx)
	case "get_account_info":
		return s.getAccountInfo(ctx, req.Address)
	default:
		return "", fmt.Errorf("solana: unknown action %q; supported: get_balance, transfer_sol, get_recent_blockhash, send_transaction, get_account_info", req.Action)
	}
}

func (s *SolanaCapability) getBalance(ctx context.Context) (string, error) {
	pubkey, err := solana.PublicKeyFromBase58(s.keypair.Base58PubKey())
	if err != nil {
		return "", fmt.Errorf("solana: invalid public key: %w", err)
	}
	result, err := s.client.GetBalance(ctx, pubkey, s.commitment)
	if err != nil {
		return "", fmt.Errorf("solana: GetBalance RPC: %w", err)
	}
	out, _ := json.Marshal(map[string]any{
		"lamports": result.Value,
		"sol":      float64(result.Value) / 1e9,
	})
	return string(out), nil
}

func (s *SolanaCapability) getRecentBlockhash(ctx context.Context) (string, error) {
	result, err := s.client.GetLatestBlockhash(ctx, s.commitment)
	if err != nil {
		return "", fmt.Errorf("solana: GetLatestBlockhash RPC: %w", err)
	}
	out, _ := json.Marshal(map[string]any{
		"blockhash":               result.Value.Blockhash.String(),
		"last_valid_block_height": result.Value.LastValidBlockHeight,
	})
	return string(out), nil
}

func (s *SolanaCapability) transferSOL(ctx context.Context, toAddr string, lamports uint64) (string, error) {
	if toAddr == "" {
		return "", fmt.Errorf("solana: transfer_sol requires 'to' address")
	}
	if lamports == 0 {
		return "", fmt.Errorf("solana: transfer_sol requires lamports > 0")
	}

	fromPubkey, err := solana.PublicKeyFromBase58(s.keypair.Base58PubKey())
	if err != nil {
		return "", fmt.Errorf("solana: invalid source public key: %w", err)
	}
	toPubkey, err := solana.PublicKeyFromBase58(toAddr)
	if err != nil {
		return "", fmt.Errorf("solana: invalid destination address %q: %w", toAddr, err)
	}

	recent, err := s.client.GetLatestBlockhash(ctx, s.commitment)
	if err != nil {
		return "", fmt.Errorf("solana: GetLatestBlockhash RPC: %w", err)
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{
			system.NewTransferInstruction(lamports, fromPubkey, toPubkey).Build(),
		},
		recent.Value.Blockhash,
		solana.TransactionPayer(fromPubkey),
	)
	if err != nil {
		return "", fmt.Errorf("solana: building transaction: %w", err)
	}

	if err := s.signTx(tx, fromPubkey); err != nil {
		return "", fmt.Errorf("solana: signing transaction: %w", err)
	}

	slog.Info("submitting SOL transfer", "from", fromPubkey.String(), "to", toAddr, "lamports", lamports)

	sig, err := s.client.SendTransaction(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("solana: SendTransaction RPC: %w", err)
	}

	slog.Info("SOL transfer submitted", "signature", sig.String(), "lamports", lamports, "to", toAddr)

	out, _ := json.Marshal(map[string]any{
		"signature": sig.String(),
		"lamports":  lamports,
		"to":        toAddr,
	})
	return string(out), nil
}

// signTx adds the agent's ed25519 signature to tx for the wallet's public key.
// Slots belonging to other signers are left untouched (nil return preserves
// existing signatures, important for protocols like Jupiter that pre-sign).
func (s *SolanaCapability) signTx(tx *solana.Transaction, signer solana.PublicKey) error {
	privKey := solana.PrivateKey(s.keypair.PrivateKeyBytes())
	_, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(signer) {
			return &privKey
		}
		return nil
	})
	return err
}

// sendTransaction deserializes a base64-encoded pre-built Solana transaction,
// adds the agent's signature, and submits it. Existing signatures from other
// signers (e.g. a Jupiter fee account) are preserved.
func (s *SolanaCapability) sendTransaction(ctx context.Context, txBase64 string) (string, error) {
	if txBase64 == "" {
		return "", fmt.Errorf("solana: send_transaction requires 'transaction' field (base64-encoded)")
	}

	txBytes, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		return "", fmt.Errorf("solana: decoding transaction base64: %w", err)
	}

	tx, err := solana.TransactionFromDecoder(bin.NewBinDecoder(txBytes))
	if err != nil {
		return "", fmt.Errorf("solana: deserializing transaction: %w", err)
	}

	fromPubkey, err := solana.PublicKeyFromBase58(s.keypair.Base58PubKey())
	if err != nil {
		return "", fmt.Errorf("solana: invalid public key: %w", err)
	}

	if err := s.signTx(tx, fromPubkey); err != nil {
		return "", fmt.Errorf("solana: signing transaction: %w", err)
	}

	slog.Info("submitting pre-built transaction", "signer", fromPubkey.String())

	sig, err := s.client.SendTransaction(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("solana: SendTransaction RPC: %w", err)
	}

	slog.Info("pre-built transaction submitted", "signature", sig.String())

	out, _ := json.Marshal(map[string]any{
		"signature": sig.String(),
	})
	return string(out), nil
}

// getAccountInfo fetches on-chain account data for the given base58 address.
// The raw account data bytes are returned base64-encoded so the LLM (or a tool)
// can decode them further if needed.
func (s *SolanaCapability) getAccountInfo(ctx context.Context, address string) (string, error) {
	if address == "" {
		return "", fmt.Errorf("solana: get_account_info requires 'address' field")
	}

	pubkey, err := solana.PublicKeyFromBase58(address)
	if err != nil {
		return "", fmt.Errorf("solana: invalid address %q: %w", address, err)
	}

	result, err := s.client.GetAccountInfoWithOpts(ctx, pubkey, &rpc.GetAccountInfoOpts{
		Encoding:   solana.EncodingBase64,
		Commitment: s.commitment,
	})
	if err != nil {
		return "", fmt.Errorf("solana: GetAccountInfo RPC: %w", err)
	}
	if result == nil || result.Value == nil {
		out, _ := json.Marshal(map[string]any{"exists": false, "address": address})
		return string(out), nil
	}

	acc := result.Value
	out, _ := json.Marshal(map[string]any{
		"exists":     true,
		"address":    address,
		"owner":      acc.Owner.String(),
		"lamports":   acc.Lamports,
		"sol":        float64(acc.Lamports) / 1e9,
		"executable": acc.Executable,
		"rent_epoch": acc.RentEpoch,
		"data":       base64.StdEncoding.EncodeToString(acc.Data.GetBinary()),
	})
	return string(out), nil
}
