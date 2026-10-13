package main

// ---- Jupiter types ----------------------------------------------------------

type jupiterEntry struct {
	UsdPrice    float64 `json:"usdPrice"`
	PriceChange float64 `json:"priceChange24h"`
}

type TokenPrice struct {
	Symbol       string  `json:"symbol"`
	Mint         string  `json:"mint"`
	PriceUSD     float64 `json:"price_usd"`
	Change24hPct float64 `json:"change_24h_pct"`
}

// ---- DexScreener types ------------------------------------------------------

type dexSearchResponse struct {
	Pairs []dexPair `json:"pairs"`
}

type dexPair struct {
	ChainID     string         `json:"chainId"`
	DexID       string         `json:"dexId"`
	PairAddress string         `json:"pairAddress"`
	BaseToken   dexToken       `json:"baseToken"`
	QuoteToken  dexToken       `json:"quoteToken"`
	PriceUSD    string         `json:"priceUsd"`
	Volume      dexVolume      `json:"volume"`
	PriceChange dexPriceChange `json:"priceChange"`
	Liquidity   dexLiquidity   `json:"liquidity"`
	FDV         float64        `json:"fdv"`
	MarketCap   float64        `json:"marketCap"`
}

type dexToken struct {
	Address string `json:"address"`
	Name    string `json:"name"`
	Symbol  string `json:"symbol"`
}

type dexVolume struct {
	H24 float64 `json:"h24"`
	H6  float64 `json:"h6"`
	H1  float64 `json:"h1"`
}

type dexPriceChange struct {
	H24 float64 `json:"h24"`
	H6  float64 `json:"h6"`
	H1  float64 `json:"h1"`
}

type dexLiquidity struct {
	USD float64 `json:"usd"`
}

// DexPairSummary is the condensed view returned to the agent.
type DexPairSummary struct {
	Symbol       string  `json:"symbol"`
	Name         string  `json:"name"`
	Mint         string  `json:"mint"`
	PriceUSD     string  `json:"price_usd"`
	Volume24hUSD float64 `json:"volume_24h_usd"`
	LiquidityUSD float64 `json:"liquidity_usd"`
	Change24hPct float64 `json:"change_24h_pct"`
	Change1hPct  float64 `json:"change_1h_pct"`
	MarketCapUSD float64 `json:"market_cap_usd,omitempty"`
	DEX          string  `json:"dex"`
}
