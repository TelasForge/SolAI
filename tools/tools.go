// Package tools registers SolAI's built-in tools.
//
// Importing this package for side effects populates the tool registry.
package tools

import (
	_ "github.com/melodyogonna/solai/tools/jupiter"
	_ "github.com/melodyogonna/solai/tools/raydium"
	_ "github.com/melodyogonna/solai/tools/solana"
	_ "github.com/melodyogonna/solai/tools/tokenprice"
	_ "github.com/melodyogonna/solai/tools/wallet"
)
