// Package llm is a minimal, dependency-free client for the OpenRouter
// Chat Completions API plus a small native-tool-calling agent loop.
//
// OpenRouter speaks the OpenAI Chat Completions protocol, so nothing here is
// OpenRouter-specific beyond the default base URL and the optional
// HTTP-Referer/X-Title attribution headers.
package llm

// Role identifies the author of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a single chat message. The same struct is used for both requests
// and responses. For assistant messages that request tools, ToolCalls is set.
// For tool-result messages, ToolCallID links back to the originating call.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a function invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// FunctionCall carries the tool name and its raw JSON arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
