package main

// ---- Raydium v3 API types ---------------------------------------------------

type poolsAPIResponse struct {
	ID      string    `json:"id"`
	Success bool      `json:"success"`
	Data    poolsData `json:"data"`
}

type poolsData struct {
	Count int        `json:"count"`
	Data  []PoolInfo `json:"data"`
}

type PoolInfo struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	MintA     TokenMint `json:"mintA"`
	MintB     TokenMint `json:"mintB"`
	Price     float64   `json:"price"`
	FeeRate   float64   `json:"feeRate"`
	TVL       float64   `json:"tvl"`
	Day       PoolStats `json:"day"`
	Week      PoolStats `json:"week"`
	Month     PoolStats `json:"month"`
	FarmCount int       `json:"farmOngoingCount"`
}

type TokenMint struct {
	Address  string `json:"address"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals int    `json:"decimals"`
}

type PoolStats struct {
	VolumeUSD float64   `json:"volumeUSD"`
	APR       float64   `json:"apr"`
	FeeAPR    float64   `json:"feeApr"`
	RewardAPR []float64 `json:"rewardApr"`
}

// PoolSummary is the condensed view returned to the agent.
type PoolSummary struct {
	PoolID       string  `json:"pool_id"`
	Type         string  `json:"type"`
	TokenA       string  `json:"token_a"`
	TokenB       string  `json:"token_b"`
	Price        float64 `json:"price"`
	TVLUSD       float64 `json:"tvl_usd"`
	Volume24hUSD float64 `json:"volume_24h_usd"`
	APR24h       float64 `json:"apr_24h_pct"`
	FeeAPR24h    float64 `json:"fee_apr_24h_pct"`
	ActiveFarms  int     `json:"active_farms"`
	FeeRate      float64 `json:"fee_rate_pct"`
}
