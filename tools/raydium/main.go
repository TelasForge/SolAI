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
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/melodyogonna/solai/ipc"
	"github.com/melodyogonna/solai/llm"
)

const raydiumPoolsBase = "https://api-v3.raydium.io/pools/info"

// knownMints maps common token symbols to their Solana mint addresses.
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

// resolveMint returns a mint address for the given input (symbol or raw address).
func resolveMint(input string) (string, bool) {
	if mint, ok := knownMints[strings.ToUpper(strings.TrimSpace(input))]; ok {
		return mint, true
	}
	// Looks like a base58 mint address
	if l := len(strings.TrimSpace(input)); l >= 32 && l <= 44 {
		return strings.TrimSpace(input), true
	}
	return "", false
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// ---- raydium-top-pools tool -------------------------------------------------

type topPoolsTool struct{}

func (t *topPoolsTool) Name() string { return "raydium-top-pools" }

func (t *topPoolsTool) Description() string {
	return `Fetch the top Raydium liquidity pools sorted by 24h volume.
Input: optional token symbol (SOL, USDC, USDT, RAY, JUP, BONK, WIF, ORCA, PYTH) or mint address to filter results; leave empty for top pools overall.
Returns: list of pools with TVL, 24h volume, APR (fee + reward), and farming status.`
}

func (t *topPoolsTool) Call(ctx context.Context, argsJSON string) (string, error) {
	slog.Info("tool call", "tool", t.Name(), "input", argsJSON)
	input := strings.TrimSpace(llm.ArgString(argsJSON, "input"))

	var apiURL string
	if mint, ok := resolveMint(input); ok {
		apiURL = raydiumPoolsBase + "/mint?mint1=" + url.QueryEscape(mint) +
			"&poolType=all&poolSortField=default&sortType=desc&pageSize=15&page=1"
	} else {
		apiURL = raydiumPoolsBase + "/list?poolType=all&poolSortField=volume24h&sortType=desc&pageSize=15&page=1"
	}

	pools, err := fetchPools(ctx, apiURL)
	if err != nil {
		return "", err
	}

	summaries := summarisePools(pools, 15)
	data, _ := json.Marshal(summaries)
	return string(data), nil
}

func (t *topPoolsTool) Parameters() map[string]any {
	return llm.StringSchema(`Optional token symbol (SOL, USDC, USDT, RAY, JUP, BONK, WIF, ORCA, PYTH) or mint address to filter by; omit for top pools overall.`)
}

// ---- raydium-search tool ----------------------------------------------------

type searchPoolsTool struct{}

func (t *searchPoolsTool) Name() string { return "raydium-search" }

func (t *searchPoolsTool) Description() string {
	return `Search Raydium for pools containing a specific token.
Input: a known token symbol (SOL, USDC, USDT, RAY, JUP, BONK, WIF, ORCA, PYTH) or a raw mint address.
Returns: matching pools with TVL, APR, volume, and farm details.`
}

func (t *searchPoolsTool) Call(ctx context.Context, argsJSON string) (string, error) {
	slog.Info("tool call", "tool", t.Name(), "input", argsJSON)
	input := strings.TrimSpace(llm.ArgString(argsJSON, "input"))
	if input == "" {
		return "", fmt.Errorf("search query is required")
	}

	mint, ok := resolveMint(input)
	if !ok {
		return "", fmt.Errorf("unrecognised token %q: provide a known symbol (%s) or a mint address",
			input, "SOL, USDC, USDT, RAY, JUP, BONK, WIF, ORCA, PYTH")
	}
	apiURL := raydiumPoolsBase + "/mint?mint1=" + url.QueryEscape(mint) +
		"&poolType=all&poolSortField=default&sortType=desc&pageSize=10&page=1"

	pools, err := fetchPools(ctx, apiURL)
	if err != nil {
		return "", err
	}

	summaries := summarisePools(pools, 10)
	data, _ := json.Marshal(summaries)
	return string(data), nil
}

func (t *searchPoolsTool) Parameters() map[string]any {
	return llm.StringSchema(`A known token symbol (SOL, USDC, USDT, RAY, JUP, BONK, WIF, ORCA, PYTH) or a raw mint address.`)
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
		&topPoolsTool{},
		&searchPoolsTool{},
	}
	agentInst := llm.NewAgent(client, tools, agentSystemPrompt,
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

	slog.Info("agent completed")
	writeSuccess(result)
}

// ---- Helpers ----------------------------------------------------------------

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
		totalAPR := p.Day.APR
		s := PoolSummary{
			PoolID:       p.ID,
			Type:         p.Type,
			TokenA:       symbolOrShort(p.MintA),
			TokenB:       symbolOrShort(p.MintB),
			Price:        p.Price,
			TVLUSD:       p.TVL,
			Volume24hUSD: p.Day.VolumeUSD,
			APR24h:       totalAPR,
			FeeAPR24h:    p.Day.FeeAPR,
			ActiveFarms:  p.FarmCount,
			FeeRate:      p.FeeRate * 100,
		}
		summaries = append(summaries, s)
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
