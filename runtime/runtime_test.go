package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/melodyogonna/solai/config"
	"github.com/melodyogonna/solai/session"
)

// fakeMCP is a minimal MCP server: initialize, tools/list, tools/call.
func fakeMCP(name, text string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		if req.ID == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}}
		case "tools/list":
			result = map[string]any{"tools": []any{
				map[string]any{
					"name":        name,
					"description": "a test tool",
					"inputSchema": map[string]any{"type": "object"},
				},
			}}
		case "tools/call":
			result = map[string]any{
				"content": []any{map[string]any{"type": "text", "text": text}},
				"isError": false,
			}
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}))
}

// fakeLLM calls mcpTool on the first turn, then resolves the goal.
func fakeLLM(t *testing.T, mcpTool string) *httptest.Server {
	t.Helper()
	turn := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		turn++
		var out string
		if turn == 1 {
			out = `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[` +
				`{"id":"c1","type":"function","function":{"name":"` + mcpTool + `","arguments":"{}"}}]}}]}`
		} else {
			out = `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[` +
				`{"id":"c2","type":"function","function":{"name":"goal","arguments":"{\"action\":\"complete\",\"note\":\"done\"}"}}]}}]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, out)
	}))
}

func TestRun_WithMCPServer(t *testing.T) {
	mcpSrv := fakeMCP("echo", "mcp-ok")
	defer mcpSrv.Close()

	llmSrv := fakeLLM(t, "demo__echo")
	defer llmSrv.Close()

	t.Setenv("SOLAI_MODEL", "test/model")
	t.Setenv("OPENROUTER_API_KEY", "test")
	t.Setenv("SOLAI_OPENROUTER_BASE_URL", llmSrv.URL)

	spec := session.Spec{
		Goals:      []string{"g"},
		Tools:      []string{"jupiter-price"},
		MCPServers: []session.MCPServer{{Name: "demo", URL: mcpSrv.URL}},
	}

	var events []session.Event
	emit := func(e session.Event) { events = append(events, e) }

	if err := Run(context.Background(), config.FromEnv(), spec, emit); err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, e := range events {
		if e.Type == session.EventToolResult && e.Tool == "demo__echo" && strings.Contains(e.Result, "mcp-ok") {
			found = true
		}
	}
	if !found {
		t.Fatalf("MCP tool not called with expected result: %+v", events)
	}
}
