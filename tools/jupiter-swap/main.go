package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/melodyogonna/solai/ipc"
	"github.com/melodyogonna/solai/llm"
	"github.com/melodyogonna/solai/ratelimit"
)

const (
	jupiterQuoteURL = "https://quote-api.jup.ag/v6/quote"
	jupiterSwapURL  = "https://quote-api.jup.ag/v6/swap"

	jupiterLiteQuoteURL = "https://lite-api.jup.ag/swap/v1/quote"
	jupiterLiteSwapURL  = "https://lite-api.jup.ag/swap/v1/swap"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// ---- Rate limiter -----------------------------------------------------------

func newJupiterLimiter() ratelimit.RateLimitStrategy {
	const defaultRPS = 1
	rps := defaultRPS
	if v := os.Getenv("JUPITER_RATE_LIMIT_RPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rps = n
		}
	}
	return ratelimit.NewFixedWindowLimiter(rps, time.Second)
}

func jupiterBaseURLs() (quoteURL, swapURL string) {
	if os.Getenv("JUPITER_API_KEY") != "" {
		return jupiterQuoteURL, jupiterSwapURL
	}
	return jupiterLiteQuoteURL, jupiterLiteSwapURL
}

// ---- Jupiter quote tool (internal agent tool) -------------------------------

type jupiterQuoteTool struct {
	limiter  ratelimit.RateLimitStrategy
	quoteURL string
}

func (t *jupiterQuoteTool) Name() string { return "jupiter-quote" }

func (t *jupiterQuoteTool) Description() string {
	return `Get the best swap route and price quote from Jupiter aggregator.
Input JSON: {"inputMint":"<mint>","outputMint":"<mint>","amount":<integer_base_units>,"slippageBps":<int>}
- amount: integer in the input token's smallest unit (lamports for SOL = 1e9, 1e6 for USDC/USDT/JUP)
- slippageBps: optional, default 50 (0.5%)
Returns: quote with expected output amount, price impact, and route details.`
}

func (t *jupiterQuoteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"inputMint":   map[string]any{"type": "string", "description": "Input token mint address."},
			"outputMint":  map[string]any{"type": "string", "description": "Output token mint address."},
			"amount":      map[string]any{"type": "integer", "description": "Amount in the input token's smallest unit (lamports for SOL)."},
			"slippageBps": map[string]any{"type": "integer", "description": "Optional slippage in basis points (default 50)."},
		},
		"required": []string{"inputMint", "outputMint", "amount"},
	}
}

func (t *jupiterQuoteTool) Call(ctx context.Context, argsJSON string) (string, error) {
	slog.Info("tool call", "tool", t.Name(), "input", argsJSON)
	input := argsJSON
	// Accept both a direct argument object and {"input":"<json string>"}.
	if s := llm.ArgString(argsJSON, "input"); s != "" && json.Valid([]byte(s)) {
		input = s
	}
	input = stripMarkdownFence(input)
	var req struct {
		InputMint   string `json:"inputMint"`
		OutputMint  string `json:"outputMint"`
		Amount      int64  `json:"amount"`
		SlippageBps int    `json:"slippageBps"`
	}
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return fmt.Sprintf("Error: invalid JSON. Expected format: {\"inputMint\":\"<mint>\",\"outputMint\":\"<mint>\",\"amount\":<integer_base_units>}. Got: %s", input), nil
	}
	if req.InputMint == "" || req.OutputMint == "" || req.Amount <= 0 {
		return fmt.Sprintf("Error: inputMint, outputMint, and amount (>0) are required. Got: %s", input), nil
	}
	if req.SlippageBps == 0 {
		req.SlippageBps = 50
	}

	if err := t.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}

	url := fmt.Sprintf("%s?inputMint=%s&outputMint=%s&amount=%d&slippageBps=%d",
		t.quoteURL, req.InputMint, req.OutputMint, req.Amount, req.SlippageBps)

	body, err := doGet(ctx, url)
	if err != nil {
		return "", fmt.Errorf("Jupiter quote: %w", err)
	}
	return string(body), nil
}

// ---- Jupiter swap-tx tool (internal agent tool) -----------------------------

type jupiterSwapTxTool struct {
	limiter       ratelimit.RateLimitStrategy
	swapURL       string
	walletAddress string
}

func (t *jupiterSwapTxTool) Name() string { return "jupiter-swap-tx" }

func (t *jupiterSwapTxTool) Description() string {
	return `Get the serialized swap transaction from Jupiter for a previously obtained quote.
Input: the complete JSON response from the jupiter-quote tool (pass it verbatim).
Returns JSON with "swapTransaction" field containing a base64-encoded unsigned transaction.
The coordinator will sign and submit this transaction via the Solana capability.`
}

func (t *jupiterSwapTxTool) Parameters() map[string]any {
	return llm.StringSchema(`The complete JSON response from jupiter-quote, passed verbatim.`)
}

func (t *jupiterSwapTxTool) Call(ctx context.Context, argsJSON string) (string, error) {
	slog.Info("tool call", "tool", t.Name(), "input_len", len(argsJSON))
	input := llm.ArgString(argsJSON, "input")
	if input == "" {
		input = argsJSON
	}
	input = stripMarkdownFence(input)
	if t.walletAddress == "" {
		return "", fmt.Errorf("wallet address not available — cannot build swap transaction")
	}

	// Validate that input looks like a Jupiter quote response.
	var quote map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &quote); err != nil {
		return "", fmt.Errorf("input must be a valid Jupiter quote JSON: %w", err)
	}
	if _, ok := quote["inputMint"]; !ok {
		return "Error: input does not look like a Jupiter quote response (missing inputMint). Pass the complete JSON output from jupiter-quote verbatim.", nil
	}

	if err := t.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}

	swapReq := swapRequest{
		QuoteResponse:             json.RawMessage(input),
		UserPublicKey:             t.walletAddress,
		DynamicComputeUnitLimit:   true,
		PrioritizationFeeLamports: "auto",
	}
	reqBody, err := json.Marshal(swapReq)
	if err != nil {
		return "", fmt.Errorf("marshalling swap request: %w", err)
	}

	apiKey := os.Getenv("JUPITER_API_KEY")
	body, err := doPost(ctx, t.swapURL, reqBody, apiKey)
	if err != nil {
		return "", fmt.Errorf("Jupiter swap: %w", err)
	}

	var resp swapResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parsing swap response: %w", err)
	}
	if resp.SwapTransaction == "" {
		return "", fmt.Errorf("Jupiter swap API returned empty transaction: %s", body)
	}

	// Return the unsigned transaction to the inner LLM. The LLM will see
	// the available capabilities section in the prompt (injected by the
	// coordinator) and can generate a capability request as its Final Answer.
	slog.Info("returning unsigned transaction to inner LLM")
	return resp.SwapTransaction, nil
}

// ---- main -------------------------------------------------------------------

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	slog.Info("tool starting", "ipc_dir", ipc.Dir())

	input, err := ipc.ReadInput()
	if err != nil {
		writeError(fmt.Sprintf("failed to read input: %v", err))
		return
	}
	slog.Info("input received", "prompt", input.Prompt, "has_wallet", input.Payload["wallet_address"] != "")

	ctx := context.Background()

	// Wallet address is pre-injected by the coordinator via Payloads.
	walletAddress := input.Payload["wallet_address"]

	quoteURL, swapURL := jupiterBaseURLs()

	slog.Info("starting agent")
	client, err := llm.FromEnv()
	if err != nil {
		slog.Error("llm init failed", "error", err)
		writeError(fmt.Sprintf("LLM initialisation failed: %v", err))
		return
	}
	slog.Info("llm initialised", "model", client.Model())

	limiter := newJupiterLimiter()
	tools := []llm.Tool{
		&jupiterQuoteTool{limiter: limiter, quoteURL: quoteURL},
		&jupiterSwapTxTool{limiter: limiter, swapURL: swapURL, walletAddress: walletAddress},
	}
	agentInst := llm.NewAgent(client, tools, buildSystemPrompt(walletAddress),
		llm.WithMaxIterations(6),
	)

	prompt := input.Prompt
	if len(input.Payload) > 0 {
		prompt += "\n\nPayloads:"
		for k, v := range input.Payload {
			prompt += fmt.Sprintf("\n- %s: %s", k, v)
		}
	}

	slog.Info("agent starting", "prompt", prompt)
	result, _, err := agentInst.Run(ctx, prompt, nil)
	if err != nil {
		slog.Error("agent failed", "error", err)
		writeError(fmt.Sprintf("agent run failed: %v", err))
		return
	}

	// Check if the agent's Final Answer is a capability request JSON.
	// The inner LLM may decide to request a coordinator capability (e.g.
	// signing a transaction) instead of producing a text result.
	var capReq ipc.Request
	if err := json.Unmarshal([]byte(result), &capReq); err == nil &&
		capReq.Capability != "" && capReq.Instruction != "" {
		slog.Info("agent produced capability request", "capability", capReq.Capability, "action", capReq.Action)
		writeRequest(capReq)
		return
	}

	slog.Info("agent completed")
	writeSuccess(result)
}

// ---- HTTP helpers -----------------------------------------------------------

func doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key := os.Getenv("JUPITER_API_KEY"); key != "" {
		req.Header.Set("x-api-key", key)
	}
	return doRequest(req)
}

func doPost(ctx context.Context, url string, body []byte, apiKey string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	return doRequest(req)
}

func doRequest(req *http.Request) ([]byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	}
	return b, nil
}

func stripMarkdownFence(s string) string {
	s = strings.TrimSpace(s)
	for _, fence := range []string{"```json", "```"} {
		if strings.HasPrefix(s, fence) {
			s = strings.TrimPrefix(s, fence)
			s = strings.TrimSuffix(strings.TrimSpace(s), "```")
			s = strings.TrimSpace(s)
			break
		}
	}
	return s
}

func writeSuccess(payload any) {
	if err := ipc.WriteSuccess(payload); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write output: %v\n", err)
	}
}

func writeError(msg string) {
	if err := ipc.WriteError(msg); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write output: %v\n", err)
	}
}

func writeRequest(req ipc.Request) {
	if err := ipc.WriteRequest(req); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write output: %v\n", err)
	}
}
