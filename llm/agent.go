package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrMaxIterations is returned by Agent.Run when the model keeps requesting
// tools without producing a final answer.
var ErrMaxIterations = errors.New("llm: agent reached maximum tool iterations")

// DefaultMaxIterations bounds how many tool-calling rounds a single Run may take.
const DefaultMaxIterations = 10

// Agent drives a native tool-calling loop against a Client.
type Agent struct {
	client        *Client
	tools         []Tool
	byName        map[string]Tool
	systemPrompt  string
	maxIterations int
}

// AgentOption customises an Agent.
type AgentOption func(*Agent)

// WithMaxIterations overrides the default tool-calling iteration cap.
func WithMaxIterations(n int) AgentOption {
	return func(a *Agent) {
		if n > 0 {
			a.maxIterations = n
		}
	}
}

// NewAgent constructs an Agent. The systemPrompt is prepended to every model
// call but is never included in the returned history.
func NewAgent(client *Client, tools []Tool, systemPrompt string, opts ...AgentOption) *Agent {
	a := &Agent{
		client:        client,
		tools:         tools,
		byName:        make(map[string]Tool, len(tools)),
		systemPrompt:  systemPrompt,
		maxIterations: DefaultMaxIterations,
	}
	for _, t := range tools {
		a.byName[t.Name()] = t
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Run executes the tool-calling loop for a single user turn.
//
// history holds prior conversation messages (without the system prompt). Run
// appends the new user message, calls the model, executes any requested tools,
// and repeats until the model returns a plain answer or maxIterations is hit.
//
// It returns the final assistant text and the updated history (which excludes
// the system prompt but includes the user message, assistant messages, and
// tool results). On error the partially-updated history is returned alongside
// the error so callers may discard or persist it as they see fit.
func (a *Agent) Run(ctx context.Context, userMessage string, history []Message) (string, []Message, error) {
	msgs := make([]Message, 0, len(history)+4)
	msgs = append(msgs, history...)
	msgs = append(msgs, Message{Role: RoleUser, Content: userMessage})

	for i := 0; i < a.maxIterations; i++ {
		if err := ctx.Err(); err != nil {
			return "", msgs, err
		}

		apiMsgs := make([]Message, 0, len(msgs)+1)
		if a.systemPrompt != "" {
			apiMsgs = append(apiMsgs, Message{Role: RoleSystem, Content: a.systemPrompt})
		}
		apiMsgs = append(apiMsgs, msgs...)

		reply, err := a.client.Chat(ctx, apiMsgs, a.tools)
		if err != nil {
			return "", msgs, err
		}
		msgs = append(msgs, *reply)

		if len(reply.ToolCalls) == 0 {
			return reply.Content, msgs, nil
		}

		for _, call := range reply.ToolCalls {
			content := a.execute(ctx, call)
			msgs = append(msgs, Message{
				Role:       RoleTool,
				ToolCallID: call.ID,
				Content:    content,
			})
		}
	}

	return "", msgs, ErrMaxIterations
}

// execute runs a single tool call and renders its result as tool-message
// content. Tool failures are returned as text so the model can observe and
// recover from them, mirroring how a ReAct "Observation" would surface errors.
func (a *Agent) execute(ctx context.Context, call ToolCall) string {
	tool, ok := a.byName[call.Function.Name]
	if !ok {
		return fmt.Sprintf("Error: unknown tool %q", call.Function.Name)
	}
	args := strings.TrimSpace(call.Function.Arguments)
	if args == "" {
		args = "{}"
	}
	out, err := tool.Call(ctx, args)
	if err != nil {
		if out != "" {
			return out + "\nError: " + err.Error()
		}
		return "Error: " + err.Error()
	}
	return out
}

// TrimHistory keeps the most recent maxUserTurns user turns (and everything
// that follows each turn) so the conversation passed to the model stays
// bounded across cycles. The cut always lands on a user-message boundary so
// assistant tool_calls never get separated from their tool results.
func TrimHistory(history []Message, maxUserTurns int) []Message {
	if maxUserTurns <= 0 {
		return history
	}
	seen := 0
	cut := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == RoleUser {
			seen++
			if seen == maxUserTurns {
				cut = i
				break
			}
		}
	}
	return history[cut:]
}
