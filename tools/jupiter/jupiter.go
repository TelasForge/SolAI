// Package jupiter provides Jupiter swap quote and transaction tools.
package jupiter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/melodyogonna/solai/config"
	"github.com/melodyogonna/solai/llm"
	"github.com/melodyogonna/solai/ratelimit"
	"github.com/melodyogonna/solai/tool"
	solwallet "github.com/melodyogonna/solai/wallet"
)

func init() {
	tool.Register("jupiter-quote",
		"Best swap route and price quote from the Jupiter aggregator.",
		func() llm.Tool { return &quoteTool{limiter: newLimiter()} })

	tool.Register("jupiter-swap",
		"Build an unsigned Jupiter swap transaction for the agent wallet.",
		func() llm.Tool {
			kp, err := solwallet.Resolve(config.FromEnv().WalletSeed)
			if err != nil {
				return &tool.ErrorTool{ToolName: "jupiter-swap", Err: err}
			}
			return &swapTool{limiter: newLimiter(), walletAddress: kp.Base58PubKey()}
		})
}

const (
	proQuoteURL = "https://quote-api.jup.ag/v6/quote"
	proSwapURL  = "https://quote-api.jup.ag/v6/swap"

	liteQuoteURL = "https://lite-api.jup.ag/swap/v1/quote"
	liteSwapURL  = "https://lite-api.jup.ag/swap/v1/swap"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

func newLimiter() ratelimit.RateLimitStrategy {
	rps := 1
	if v := os.Getenv("JUPITER_RATE_LIMIT_RPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rps = n
		}
	}
	return ratelimit.NewFixedWindowLimiter(rps, time.Second)
}

func baseURLs() (quoteURL, swapURL string) {
	if os.Getenv("JUPITER_API_KEY") != "" {
		return proQuoteURL, proSwapURL
	}
	return liteQuoteURL, liteSwapURL
}

type quoteArgs struct {
	InputMint   string `json:"inputMint"`
	OutputMint  string `json:"outputMint"`
	Amount      int64  `json:"amount"`
	SlippageBps int    `json:"slippageBps"`
}

func quoteParameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"inputMint":   map[string]any{"type": "string", "description": "Input token mint address."},
			"outputMint":  map[string]any{"type": "string", "description": "Output token mint address."},
			"amount":      map[string]any{"type": "integer", "description": "Amount in the input token's smallest unit."},
			"slippageBps": map[string]any{"type": "integer", "description": "Slippage in basis points (default 50)."},
		},
		"required": []string{"inputMint", "outputMint", "amount"},
	}
}

func parseQuoteArgs(argsJSON string) (quoteArgs, error) {
	var a quoteArgs
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return a, fmt.Errorf("invalid arguments: %w", err)
	}
	if a.InputMint == "" || a.OutputMint == "" || a.Amount <= 0 {
		return a, fmt.Errorf("inputMint, outputMint, and amount (>0) are required")
	}
	if a.SlippageBps == 0 {
		a.SlippageBps = 50
	}
	return a, nil
}

// ---- jupiter-quote ----------------------------------------------------------

type quoteTool struct{ limiter ratelimit.RateLimitStrategy }

func (t *quoteTool) Name() string { return "jupiter-quote" }

func (t *quoteTool) Description() string {
	return "Best swap route and price quote from Jupiter, with expected output amount and price impact."
}

func (t *quoteTool) Parameters() map[string]any { return quoteParameters() }

func (t *quoteTool) Call(ctx context.Context, argsJSON string) (string, error) {
	a, err := parseQuoteArgs(argsJSON)
	if err != nil {
		return "", err
	}
	if err := t.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}
	quoteURL, _ := baseURLs()
	url := fmt.Sprintf("%s?inputMint=%s&outputMint=%s&amount=%d&slippageBps=%d",
		quoteURL, a.InputMint, a.OutputMint, a.Amount, a.SlippageBps)
	body, err := doGet(ctx, url)
	if err != nil {
		return "", fmt.Errorf("Jupiter quote: %w", err)
	}
	return string(body), nil
}

// ---- jupiter-swap -----------------------------------------------------------

type swapTool struct {
	limiter       ratelimit.RateLimitStrategy
	walletAddress string
}

func (t *swapTool) Name() string { return "jupiter-swap" }

func (t *swapTool) Description() string {
	return "Build an unsigned Jupiter swap transaction for the agent wallet. Returns a base64 transaction; " +
		"submit it with the solana tool's send_transaction action."
}

func (t *swapTool) Parameters() map[string]any { return quoteParameters() }

func (t *swapTool) Call(ctx context.Context, argsJSON string) (string, error) {
	a, err := parseQuoteArgs(argsJSON)
	if err != nil {
		return "", err
	}
	if err := t.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}

	quoteURL, swapURL := baseURLs()
	quoteURL = fmt.Sprintf("%s?inputMint=%s&outputMint=%s&amount=%d&slippageBps=%d",
		quoteURL, a.InputMint, a.OutputMint, a.Amount, a.SlippageBps)
	quoteBody, err := doGet(ctx, quoteURL)
	if err != nil {
		return "", fmt.Errorf("Jupiter quote: %w", err)
	}

	swapReq := swapRequest{
		QuoteResponse:             json.RawMessage(quoteBody),
		UserPublicKey:             t.walletAddress,
		DynamicComputeUnitLimit:   true,
		PrioritizationFeeLamports: "auto",
	}
	reqBody, err := json.Marshal(swapReq)
	if err != nil {
		return "", fmt.Errorf("marshalling swap request: %w", err)
	}
	body, err := doPost(ctx, swapURL, reqBody, os.Getenv("JUPITER_API_KEY"))
	if err != nil {
		return "", fmt.Errorf("Jupiter swap: %w", err)
	}

	var resp swapResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parsing swap response: %w", err)
	}
	if resp.SwapTransaction == "" {
		return "", fmt.Errorf("Jupiter swap API returned an empty transaction")
	}
	out, _ := json.Marshal(map[string]string{"swapTransaction": resp.SwapTransaction})
	return string(out), nil
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
