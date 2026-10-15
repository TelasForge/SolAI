// Package tokenprice provides Jupiter price and DexScreener market tools.
package tokenprice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/melodyogonna/solai/llm"
	"github.com/melodyogonna/solai/ratelimit"
	"github.com/melodyogonna/solai/tool"
)

func init() {
	tool.Register("jupiter-price",
		"Current USD prices and 24h change for Solana tokens (Jupiter).",
		func() llm.Tool { return &jupiterPriceTool{limiter: newLimiter()} })
	tool.Register("dexscreener-search",
		"Search Solana tokens on DexScreener by name, symbol, or keyword.",
		func() llm.Tool { return &dexSearchTool{} })
	tool.Register("dexscreener-token",
		"Detailed market data for Solana tokens by mint address (DexScreener).",
		func() llm.Tool { return &dexTokenTool{} })
}

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
}

var mintToSymbol = func() map[string]string {
	m := make(map[string]string, len(knownTokens))
	for sym, mint := range knownTokens {
		m[mint] = sym
	}
	return m
}()

var httpClient = &http.Client{Timeout: 10 * time.Second}

func newLimiter() ratelimit.RateLimitStrategy {
	rps := 1
	if v := os.Getenv("JUPITER_RATE_LIMIT_RPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rps = n
		}
	}
	return ratelimit.NewFixedWindowLimiter(rps, time.Second)
}

// ---- jupiter-price ----------------------------------------------------------

type jupiterPriceTool struct{ limiter ratelimit.RateLimitStrategy }

func (t *jupiterPriceTool) Name() string { return "jupiter-price" }

func (t *jupiterPriceTool) Description() string {
	return "Current USD prices and 24h change for Solana tokens. Best for accurate prices of well-known tokens."
}

func (t *jupiterPriceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tokens": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Token symbols (e.g. SOL, USDC, JUP) or mint addresses.",
			},
		},
		"required": []string{"tokens"},
	}
}

func (t *jupiterPriceTool) Call(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Tokens []string `json:"tokens"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	mints := resolveToMints(args.Tokens)
	if len(mints) == 0 {
		return "", fmt.Errorf("no recognised token symbols or mint addresses")
	}
	if err := t.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}
	prices, err := fetchJupiterPrices(ctx, mints)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(prices)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ---- dexscreener-search -----------------------------------------------------

type dexSearchTool struct{}

func (t *dexSearchTool) Name() string { return "dexscreener-search" }

func (t *dexSearchTool) Description() string {
	return "Search Solana tokens on DexScreener by name, symbol, or keyword. Best for discovering new or unknown tokens."
}

func (t *dexSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": `e.g. "BONK", "dogwifhat", "meme coin"`},
		},
		"required": []string{"query"},
	}
}

func (t *dexSearchTool) Call(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", fmt.Errorf("query is required")
	}
	apiURL := "https://api.dexscreener.com/latest/dex/search?q=" + url.QueryEscape(args.Query)
	body, err := getJSON(ctx, apiURL)
	if err != nil {
		return "", err
	}
	var resp dexSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parsing DexScreener response: %w", err)
	}
	data, _ := json.Marshal(filterAndSummarise(resp.Pairs, 8))
	return string(data), nil
}

// ---- dexscreener-token ------------------------------------------------------

type dexTokenTool struct{}

func (t *dexTokenTool) Name() string { return "dexscreener-token" }

func (t *dexTokenTool) Description() string {
	return "Detailed market data for Solana tokens by mint address. Best when you already have a mint and need pool-level detail."
}

func (t *dexTokenTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mints": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Mint addresses (max 10).",
			},
		},
		"required": []string{"mints"},
	}
}

func (t *dexTokenTool) Call(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Mints []string `json:"mints"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	addrs := make([]string, 0, 10)
	for _, m := range args.Mints {
		if m = strings.TrimSpace(m); m != "" {
			addrs = append(addrs, m)
		}
		if len(addrs) == 10 {
			break
		}
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("at least one mint address is required")
	}
	apiURL := "https://api.dexscreener.com/latest/dex/tokens/" + strings.Join(addrs, ",")
	body, err := getJSON(ctx, apiURL)
	if err != nil {
		return "", err
	}
	var resp dexSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parsing DexScreener response: %w", err)
	}
	data, _ := json.Marshal(filterAndSummarise(resp.Pairs, 10))
	return string(data), nil
}

// ---- helpers ----------------------------------------------------------------

func resolveToMints(tokens []string) []string {
	seen := make(map[string]bool)
	var mints []string
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		upper := strings.ToUpper(t)
		switch {
		case knownTokens[upper] != "":
			if mint := knownTokens[upper]; !seen[mint] {
				seen[mint] = true
				mints = append(mints, mint)
			}
		case len(t) >= 32 && len(t) <= 44:
			if !seen[t] {
				seen[t] = true
				mints = append(mints, t)
			}
		}
	}
	return mints
}

func fetchJupiterPrices(ctx context.Context, mints []string) ([]TokenPrice, error) {
	baseURL := "https://lite-api.jup.ag/price/v3"
	apiKey := os.Getenv("JUPITER_API_KEY")
	if apiKey != "" {
		baseURL = "https://api.jup.ag/price/v3"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"?ids="+strings.Join(mints, ","), nil)
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
			sym = mint[:min(8, len(mint))]
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
