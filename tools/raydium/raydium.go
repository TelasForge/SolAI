// Package raydium provides Raydium liquidity-pool tools.
package raydium

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/melodyogonna/solai/llm"
	"github.com/melodyogonna/solai/tool"
)

func init() {
	tool.Register("raydium-top-pools",
		"Top Raydium liquidity pools by 24h volume, optionally filtered by token.",
		func() llm.Tool { return &topPoolsTool{} })
	tool.Register("raydium-search",
		"Raydium pools containing a specific token.",
		func() llm.Tool { return &searchPoolsTool{} })
}

const raydiumPoolsBase = "https://api-v3.raydium.io/pools/info"

var knownMints = map[string]string{
	"SOL":  "So11111111111111111111111111111111111111112",
	"USDC": "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
	"USDT": "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB",
	"RAY":  "4k3Dyjzvzp8eMZWUXbBCjEvwSkkk59S5iCNLY3QrkX6R",
	"JUP":  "JUPyiwrYJFskUPiHa7hkeR8VUtAeFoSYbKedZNsDvCN",
	"BONK": "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263",
	"WIF":  "EKpQGSJtjMFqKZ9KQanSqYXRcF8fBopzLHYxdM65zcjm",
	"ORCA": "orcaEKTdK7LKz57vaAYr9QeNsVEPfiu6QeMU1kektZE",
	"PYTH": "HZ1JovNiVvGrGNiiYvEozEVgZ58xaU3RKwX8eACQBCt3",
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func resolveMint(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if mint := knownMints[strings.ToUpper(input)]; mint != "" {
		return mint, true
	}
	if l := len(input); l >= 32 && l <= 44 {
		return input, true
	}
	return "", false
}

// ---- raydium-top-pools ------------------------------------------------------

type topPoolsTool struct{}

func (t *topPoolsTool) Name() string { return "raydium-top-pools" }

func (t *topPoolsTool) Description() string {
	return "Top Raydium pools by 24h volume, with TVL, volume, APR, and farming status."
}

func (t *topPoolsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"token": map[string]any{"type": "string", "description": "Optional symbol or mint address to filter by."},
		},
	}
}

func (t *topPoolsTool) Call(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	var apiURL string
	if mint, ok := resolveMint(args.Token); ok {
		apiURL = raydiumPoolsBase + "/mint?mint1=" + url.QueryEscape(mint) +
			"&poolType=all&poolSortField=default&sortType=desc&pageSize=15&page=1"
	} else {
		apiURL = raydiumPoolsBase + "/list?poolType=all&poolSortField=volume24h&sortType=desc&pageSize=15&page=1"
	}

	pools, err := fetchPools(ctx, apiURL)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(summarisePools(pools, 15))
	return string(data), nil
}

// ---- raydium-search ---------------------------------------------------------

type searchPoolsTool struct{}

func (t *searchPoolsTool) Name() string { return "raydium-search" }

func (t *searchPoolsTool) Description() string {
	return "Raydium pools containing a specific token, with TVL, APR, volume, and farm details."
}

func (t *searchPoolsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"token": map[string]any{"type": "string", "description": "Token symbol or mint address."},
		},
		"required": []string{"token"},
	}
}

func (t *searchPoolsTool) Call(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Token) == "" {
		return "", fmt.Errorf("token is required")
	}
	mint, ok := resolveMint(args.Token)
	if !ok {
		return "", fmt.Errorf("unrecognised token %q: provide a known symbol or a mint address", args.Token)
	}
	apiURL := raydiumPoolsBase + "/mint?mint1=" + url.QueryEscape(mint) +
		"&poolType=all&poolSortField=default&sortType=desc&pageSize=10&page=1"
	pools, err := fetchPools(ctx, apiURL)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(summarisePools(pools, 10))
	return string(data), nil
}

// ---- helpers ----------------------------------------------------------------

func fetchPools(ctx context.Context, apiURL string) ([]PoolInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Raydium API request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Raydium API returned HTTP %d: %s", resp.StatusCode, body)
	}

	var apiResp poolsAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing Raydium response: %w", err)
	}
	if !apiResp.Success {
		return nil, fmt.Errorf("Raydium API returned success=false")
	}
	return apiResp.Data.Data, nil
}

func summarisePools(pools []PoolInfo, n int) []PoolSummary {
	summaries := make([]PoolSummary, 0, n)
	for _, p := range pools {
		if len(summaries) == n {
			break
		}
		summaries = append(summaries, PoolSummary{
			PoolID:       p.ID,
			Type:         p.Type,
			TokenA:       symbolOrShort(p.MintA),
			TokenB:       symbolOrShort(p.MintB),
			Price:        p.Price,
			TVLUSD:       p.TVL,
			Volume24hUSD: p.Day.VolumeUSD,
			APR24h:       p.Day.APR,
			FeeAPR24h:    p.Day.FeeAPR,
			ActiveFarms:  p.FarmCount,
			FeeRate:      p.FeeRate * 100,
		})
	}
	return summaries
}

func symbolOrShort(m TokenMint) string {
	if m.Symbol != "" {
		return m.Symbol
	}
	if len(m.Address) >= 8 {
		return m.Address[:8] + "…"
	}
	return m.Address
}
