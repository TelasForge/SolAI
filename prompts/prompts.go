// Package prompts embeds the agent system prompt into the binary.
package prompts

import _ "embed"

//go:embed system.md
var SystemPrompt string
