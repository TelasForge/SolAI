package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type echoTool struct{ called *[]string }

func (t echoTool) Name() string        { return "echo" }
func (t echoTool) Description() string { return "echoes input" }
func (t echoTool) Call(_ context.Context, argsJSON string) (string, error) {
	*t.called = append(*t.called, argsJSON)
	return "echo:" + ArgString(argsJSON, "input"), nil
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := NewClient(Config{APIKey: "test", BaseURL: srv.URL, Model: "test/model", MaxRetries: 1})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestClient_Chat_ParsesAssistantMessage(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test" {
			t.Errorf("auth header: got %q", got)
		}
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message      Message `json:"message"`
				FinishReason string  `json:"finish_reason"`
			}{{Message: Message{Role: RoleAssistant, Content: "hello"}}},
		})
	})

	msg, err := c.Chat(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if msg.Content != "hello" {
		t.Errorf("content: got %q", msg.Content)
	}
}

func TestClient_Chat_RetriesOn429(t *testing.T) {
	attempts := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message      Message `json:"message"`
				FinishReason string  `json:"finish_reason"`
			}{{Message: Message{Role: RoleAssistant, Content: "ok"}}},
		})
	})

	msg, err := c.Chat(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if msg.Content != "ok" {
		t.Errorf("content: got %q", msg.Content)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestAgent_Run_ExecutesToolThenAnswers(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if calls == 1 {
			// Verify the tool schema was advertised.
			if len(req.Tools) != 1 || req.Tools[0].Function.Name != "echo" {
				t.Fatalf("expected echo tool advertised, got %+v", req.Tools)
			}
			_ = json.NewEncoder(w).Encode(chatResponse{
				Choices: []struct {
					Message      Message `json:"message"`
					FinishReason string  `json:"finish_reason"`
				}{{Message: Message{
					Role: RoleAssistant,
					ToolCalls: []ToolCall{{
						ID:       "call_1",
						Type:     "function",
						Function: FunctionCall{Name: "echo", Arguments: `{"input":"ping"}`},
					}},
				}}},
			})
			return
		}
		// Second call must include the tool result.
		found := false
		for _, m := range req.Messages {
			if m.Role == RoleTool && m.ToolCallID == "call_1" && m.Content == "echo:ping" {
				found = true
			}
		}
		if !found {
			t.Fatalf("tool result not present in follow-up messages: %+v", req.Messages)
		}
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message      Message `json:"message"`
				FinishReason string  `json:"finish_reason"`
			}{{Message: Message{Role: RoleAssistant, Content: "final"}}},
		})
	})

	var called []string
	agent := NewAgent(c, []Tool{echoTool{called: &called}}, "system")
	answer, history, err := agent.Run(context.Background(), "go", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "final" {
		t.Errorf("answer: got %q", answer)
	}
	if len(called) != 1 || called[0] != `{"input":"ping"}` {
		t.Errorf("tool calls: got %v", called)
	}
	if len(history) != 4 { // user, assistant(tool_call), tool, assistant(final)
		t.Errorf("history len: got %d, want 4", len(history))
	}
}

func TestTrimHistory_KeepsRecentUserTurns(t *testing.T) {
	history := []Message{
		{Role: RoleUser, Content: "u1"},
		{Role: RoleAssistant, Content: "a1"},
		{Role: RoleUser, Content: "u2"},
		{Role: RoleAssistant, Content: "a2"},
		{Role: RoleUser, Content: "u3"},
		{Role: RoleAssistant, Content: "a3"},
	}
	got := TrimHistory(history, 2)
	if len(got) != 4 || got[0].Content != "u2" {
		t.Errorf("unexpected trim result: %+v", got)
	}
}

func TestArgString(t *testing.T) {
	if got := ArgString(`{"input":"a,b"}`, "input"); got != "a,b" {
		t.Errorf("got %q", got)
	}
	if got := ArgString("bare", "input"); got != "bare" {
		t.Errorf("got %q", got)
	}
	if got := ArgString(`{"other":"x"}`, "input"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestStringSchema_Shape(t *testing.T) {
	s := StringSchema("desc")
	if s["type"] != "object" {
		t.Errorf("type: got %v", s["type"])
	}
	props, ok := s["properties"].(map[string]any)
	if !ok || props["input"] == nil {
		t.Errorf("missing input property: %v", s)
	}
}

func TestBuildToolDefs_UsesParameters(t *testing.T) {
	defs := buildToolDefs([]Tool{paramTool{}})
	if len(defs) != 1 {
		t.Fatalf("got %d defs", len(defs))
	}
	if defs[0].Function.Parameters["type"] != "string" {
		t.Errorf("custom schema not used: %v", defs[0].Function.Parameters)
	}
}

type paramTool struct{}

func (paramTool) Name() string        { return "p" }
func (paramTool) Description() string { return "d" }
func (paramTool) Call(context.Context, string) (string, error) {
	return "", nil
}
func (paramTool) Parameters() map[string]any {
	return map[string]any{"type": "string", "description": "custom"}
}

func TestClient_RequiresAPIKey(t *testing.T) {
	if _, err := NewClient(Config{}); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("expected API key error, got %v", err)
	}
}
