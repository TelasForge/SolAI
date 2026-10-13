package llm

import (
	"context"
	"encoding/json"
	"strings"
)

// Tool is anything the agent can call during its reasoning loop.
//
// Call receives the raw JSON arguments object produced by the model, e.g.
// `{"input":"SOL,USDC"}` or `{"action":"get_balance"}`. Tools parse it
// themselves (see ArgString / ArgRaw).
type Tool interface {
	Name() string
	Description() string
	Call(ctx context.Context, argsJSON string) (string, error)
}

// ParameterizedTool optionally advertises a JSON Schema for its arguments.
// When a tool does not implement this interface, the agent advertises a single
// optional free-text "input" string property.
type ParameterizedTool interface {
	Parameters() map[string]any
}

// StringSchema returns a JSON Schema for a tool that takes one free-text input.
func StringSchema(description string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": description,
			},
		},
	}
}

// OpenObjectSchema returns a JSON Schema that accepts arbitrary named
// properties. Used for capabilities whose argument shape is documented in the
// tool description rather than in a rigid schema.
func OpenObjectSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
	}
}

// ArgString extracts a string field from a raw JSON arguments object. If the
// input is not a JSON object it is returned trimmed as-is, so callers can
// accept both `{"input":"x"}` and a bare `x`.
func ArgString(argsJSON, key string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &obj); err != nil {
		return strings.TrimSpace(argsJSON)
	}
	raw, ok := obj[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

// ArgRaw returns the raw JSON value for a field, or nil when absent/unparseable.
func ArgRaw(argsJSON, key string) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &obj); err != nil {
		return nil
	}
	return obj[key]
}
