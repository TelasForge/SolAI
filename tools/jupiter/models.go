package jupiter

import "encoding/json"

// ---- Jupiter API types ------------------------------------------------------

type swapRequest struct {
	QuoteResponse             json.RawMessage `json:"quoteResponse"`
	UserPublicKey             string          `json:"userPublicKey"`
	DynamicComputeUnitLimit   bool            `json:"dynamicComputeUnitLimit"`
	PrioritizationFeeLamports string          `json:"prioritizationFeeLamports"`
}

type swapResponse struct {
	SwapTransaction string `json:"swapTransaction"`
}
