package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/melodyogonna/solai/ipc"
	"github.com/melodyogonna/solai/llm"
	"github.com/melodyogonna/solai/ratelimit"
)

var knownTokens = map[string]string{
	"SOL":  "So11111111111111111111111111111111111111112",
	"USDC": "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
	"USDT": "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB",
	"JUP":  "JUPyiwrYJFskUPiHa7hkeR8VUtAeFoSYbKedZNsDvCN",
	"BONK": "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
	"WIF":  "EKpQGSJtjMFqKZ9KQanSqYXRcF8fBopzLHYxdM65zcjm",
	"PYTH": "HZ1JovNiVvGrGNiiYvEozEVgZ58xaU3RKwX8eACQBCt3",
	"RAY":  "4k3Dyjzvzp8eMZWUXbBCjEvwSkkk59S5iCNLY3QrkX6R",
	"ORCA": "orcaEKTdK7LKz57vaAYr9QeNsVEPfiu6QeMU1kektZE",
	"MNGO": "MangoCzJ36AjZyKwVj3VnYU4GTonjfVEnJmvvWaxLac",
	"SAMO": "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
}

var mintToSymbol = func() map[string]string {
	m := make(map[string]string, len(knownTokens))
	for sym, mint := range knownTokens {
		m[mint] = sym
	}
	return m
}()

var httpClient = &http.Client{Timeout: 10 * time.Second}

// ---- Jupiter price tool -----------------------------------------------------

type jupiterTool struct {
	limiter ratelimit.RateLimitStrategy
}

func (t *jupiterTool) Name() string { return "jupiter-price" }

func (t *jupiterTool) Description() string {
	syms := make([]string, 0, len(knownTokens))
	for s := range knownTokens {
		syms = append(syms, s)
	}
	return fmt.Sprintf(
		`Fetch current USD prices and 24h change for Solana tokens from Jupiter.
Input: comma-separated token symbols (%s) or raw mint addresses.
Example: "SOL,USDC,JUP"
Returns JSON with price_usd and change_24h_pct. Best for accurate prices of well-known tokens.`,
		strings.Join(syms, ", "),
	)
}

func (t *jupiterTool) Call(ctx context.Context, argsJSON string) (string, error) {
	slog.Info("tool call", "tool", t.Name(), "input", argsJSON)
	input := llm.ArgString(argsJSON, "input")
	if input == "" {
		input = stripMarkdownFence(argsJSON)
	}
	mints := resolveToMints(parseTokenList(input))
	if len(mints) == 0 {
		return "", fmt.Errorf("no recognised token symbols or mint addresses in: %q", input)
	}
	if err := t.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}
	prices, err := fetchJupiterPrices(mints)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(prices)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ---- DexScreener search tool ------------------------------------------------

type dexSearchTool struct{}

func (t *jupiterTool) Parameters() map[string]any {
	return llm.StringSchema(`Comma-separated token symbols (e.g. "SOL,USDC,JUP") or raw mint addresses.`)
}

func (t *dexSearchTool) Name() string { return "dexscreener-search" }

func (t *dexSearchTool) Description() string {
	return `Search for Solana tokens on DexScreener by name, symbol, or keyword.
Input: a search query (e.g. "BONK", "dogwifhat", "meme coin").
Returns top Solana pairs with price, 24h volume, liquidity, and price change.
Best for discovering tokens, finding new/unknown tokens, or market discovery.`
}

func (t *dexSearchTool) Call(ctx context.Context, argsJSON string) (string, error) {
	slog.Info("tool call", "tool", t.Name(), "input", argsJSON)
	input := strings.TrimSpace(llm.ArgString(argsJSON, "input"))
	if input == "" {
		input = strings.TrimSpace(stripMarkdownFence(argsJSON))
	}
	// If LLM passed JSON like {"query":"..."} or {"q":"..."}, extract the value.
	var obj map[string]string
	if json.Unmarshal([]byte(input), &obj) == nil {
		for _, k := range []string{"query", "q", "input", "search"} {
			if v, ok := obj[k]; ok {
				input = v
				break
			}
		}
	}
	if input == "" {
		return "", fmt.Errorf("search query is required")
	}
	apiURL := "https://api.dexscreener.com/latest/dex/search?q=" + url.QueryEscape(input)
	body, err := getJSON(ctx, apiURL)
	if err != nil {
		return "", err
	}
	var resp dexSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parsing DexScreener response: %w", err)
	}
	summaries := filterAndSummarise(resp.Pairs, 8)
	data, _ := json.Marshal(summaries)
	return string(data), nil
}

func (t *dexSearchTool) Parameters() map[string]any {
	return llm.StringSchema(`A search query (e.g. "BONK", "dogwifhat", "meme coin").`)
}

// ---- DexScreener token tool -------------------------------------------------

type dexTokenTool struct{}

func (t *dexTokenTool) Name() string { return "dexscreener-token" }

func (t *dexTokenTool) Description() string {
	return `Get detailed market data for Solana tokens by mint address from DexScreener.
Input: comma-separated mint addresses (max 10).
Returns pools, price, volume, and liquidity across all DEXes for those tokens.
Best when you already have a mint address and need pool-level market detail.`
}

func (t *dexTokenTool) Call(ctx context.Context, argsJSON string) (string, error) {
	slog.Info("tool call", "tool", t.Name(), "input", argsJSON)
	input := llm.ArgString(argsJSON, "input")
	if input == "" {
		input = argsJSON
	}
	parts := parseTokenList(stripMarkdownFence(input))
	var addresses []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			addresses = append(addresses, p)
		}
		if len(addresses) == 10 {
			break
		}
	}
	if len(addresses) == 0 {
		return "", fmt.Errorf("at least one mint address is required")
	}
	apiURL := "https://api.dexscreener.com/latest/dex/tokens/" + strings.Join(addresses, ",")
	body, err := getJSON(ctx, apiURL)
	if err != nil {
		return "", err
	}
	var resp dexSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parsing DexScreener response: %w", err)
	}
	summaries := filterAndSummarise(resp.Pairs, 10)
	data, _ := json.Marshal(summaries)
	return string(data), nil
}

func (t *dexTokenTool) Parameters() map[string]any {
	return llm.StringSchema(`Comma-separated mint addresses (max 10).`)
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
	slog.Info("input received", "prompt", input.Prompt)

	ctx := context.Background()

	client, err := llm.FromEnv()
	if err != nil {
		slog.Error("llm init failed", "error", err)
		writeError(fmt.Sprintf("LLM initialisation failed: %v", err))
		return
	}
	slog.Info("llm initialised", "model", client.Model())

	tools := []llm.Tool{
		&jupiterTool{limiter: newJupiterLimiter()},
		&dexSearchTool{},
		&dexTokenTool{},
	}
	agentInst := llm.NewAgent(client, tools, agentSystemPrompt,
		llm.WithMaxIterations(8),
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

	slog.Info("agent completed")
	writeSuccess(result)
}

// ---- Helpers ----------------------------------------------------------------

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

func resolveToMints(tokens []string) []string {
	seen := make(map[string]bool)
	var mints []string
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		upper := strings.ToUpper(t)
		if mint, ok := knownTokens[upper]; ok {
			if !seen[mint] {
				seen[mint] = true
				mints = append(mints, mint)
			}
		} else if len(t) >= 32 && len(t) <= 44 {
			if !seen[t] {
				seen[t] = true
				mints = append(mints, t)
			}
		}
	}
	return mints
}

func fetchJupiterPrices(mints []string) ([]TokenPrice, error) {
	baseURL := "https://lite-api.jup.ag/price/v3"
	apiKey := os.Getenv("JUPITER_API_KEY")
	if apiKey != "" {
		baseURL = "https://api.jup.ag/price/v3"
	}

	req, err := http.NewRequest(http.MethodGet, baseURL+"?ids="+strings.Join(mints, ","), nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Jupiter HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Jupiter API returned HTTP %d: %s", resp.StatusCode, body)
	}

	var jupResp map[string]jupiterEntry
	if err := json.Unmarshal(body, &jupResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	prices := make([]TokenPrice, 0, len(mints))
	for _, mint := range mints {
		entry, ok := jupResp[mint]
		if !ok {
			continue
		}
		sym := mintToSymbol[mint]
		if sym == "" {
			sym = mint[:8] + "…"
		}
		prices = append(prices, TokenPrice{
			Symbol:       sym,
			Mint:         mint,
			PriceUSD:     entry.UsdPrice,
			Change24hPct: entry.PriceChange,
		})
	}
	return prices, nil
}

func getJSON(ctx context.Context, apiURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request to %s: %w", apiURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

// filterAndSummarise filters pairs to Solana-only and returns the top n summaries.
func filterAndSummarise(pairs []dexPair, n int) []DexPairSummary {
	var summaries []DexPairSummary
	for _, p := range pairs {
		if p.ChainID != "solana" {
			continue
		}
		summaries = append(summaries, DexPairSummary{
			Symbol:       p.BaseToken.Symbol,
			Name:         p.BaseToken.Name,
			Mint:         p.BaseToken.Address,
			PriceUSD:     p.PriceUSD,
			Volume24hUSD: p.Volume.H24,
			LiquidityUSD: p.Liquidity.USD,
			Change24hPct: p.PriceChange.H24,
			Change1hPct:  p.PriceChange.H1,
			MarketCapUSD: p.MarketCap,
			DEX:          p.DexID,
		})
		if len(summaries) == n {
			break
		}
	}
	return summaries
}

// stripMarkdownFence removes ```json ... ``` or ``` ... ``` wrappers that the
// LLM sometimes wraps tool inputs in.
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

// parseTokenList accepts either a comma-separated string or a JSON object/array
// with token symbols or mint addresses and returns a flat slice of strings.
func parseTokenList(input string) []string {
	input = strings.TrimSpace(input)
	// Try JSON array: ["SOL","USDC",...]
	var arr []string
	if json.Unmarshal([]byte(input), &arr) == nil {
		return arr
	}
	// Try JSON object: {"mints":[...]} / {"tokens":[...]} / {"ids":[...]}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(input), &obj) == nil {
		for _, k := range []string{"mints", "tokens", "ids", "symbols", "addresses"} {
			if raw, ok := obj[k]; ok {
				var list []string
				if json.Unmarshal(raw, &list) == nil {
					return list
				}
			}
		}
	}
	// Fall back to comma-separated.
	return strings.Split(input, ",")
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
