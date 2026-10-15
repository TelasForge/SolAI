package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type rpcIn struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func writeResult(w http.ResponseWriter, id int64, result any) {
	res, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(res)
}

func sseResult(w http.ResponseWriter, id int64, result any) {
	res, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = w.Write([]byte("event: message\ndata: " + string(res) + "\n\n"))
}

func fakeServer(t *testing.T, useSSE bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req rpcIn
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.ID == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		reply := func(result any) {
			if useSSE {
				sseResult(w, req.ID, result)
			} else {
				writeResult(w, req.ID, result)
			}
		}

		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-1")
			reply(map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}})
		case "tools/list":
			reply(map[string]any{"tools": []any{
				map[string]any{
					"name":        "echo",
					"description": "Echo text back.",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{"text": map[string]any{"type": "string"}},
						"required":   []string{"text"},
					},
				},
			}})
		case "tools/call":
			var p struct {
				Name      string            `json:"name"`
				Arguments map[string]string `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			reply(map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "echo:" + p.Arguments["text"]}},
				"isError": false,
			})
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":`+strconv.FormatInt(req.ID, 10)+`,"error":{"code":-32601,"message":"method not found"}}`)
		}
	}))
}

func TestConnect_ToolsAndCall(t *testing.T) {
	srv := fakeServer(t, false)
	defer srv.Close()

	client, err := Connect(context.Background(), []Server{{Name: "test", URL: srv.URL}})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	tools := client.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0]
	if got := tool.Name(); got != "test__echo" {
		t.Errorf("Name: got %q, want test__echo", got)
	}
	if got := tool.Description(); got != "Echo text back." {
		t.Errorf("Description: got %q", got)
	}
	if _, ok := tool.(interface{ Parameters() map[string]any }); !ok {
		t.Fatal("tool should advertise parameters")
	}

	out, err := tool.Call(context.Background(), `{"text":"hello"}`)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out != "echo:hello" {
		t.Errorf("Call: got %q", out)
	}
}

func TestConnect_SSEResponse(t *testing.T) {
	srv := fakeServer(t, true)
	defer srv.Close()

	client, err := Connect(context.Background(), []Server{{Name: "sse", URL: srv.URL}})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	if len(client.Tools()) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(client.Tools()))
	}
}

func TestConnect_RequiresNameAndURL(t *testing.T) {
	if _, err := Connect(context.Background(), []Server{{URL: "http://x"}}); err == nil {
		t.Error("expected error when name is missing")
	}
	if _, err := Connect(context.Background(), []Server{{Name: "x"}}); err == nil {
		t.Error("expected error when URL is missing")
	}
}

func TestConnect_Unreachable(t *testing.T) {
	if _, err := Connect(context.Background(), []Server{{Name: "x", URL: "http://127.0.0.1:0/mcp"}}); err == nil {
		t.Error("expected connect error")
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"quote":         "quote",
		"foo.bar":       "foo_bar",
		"a/b:c":         "a_b_c",
		"already_ok-12": "already_ok-12",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q): got %q, want %q", in, got, want)
		}
	}
}

func TestParseSSE(t *testing.T) {
	data := "event: message\ndata: {\"a\":1}\n\ndata: {\"b\":2}\n\n"
	msgs := parseSSE([]byte(data))
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %v", len(msgs), msgs)
	}
	if string(msgs[1]) != `{"b":2}` {
		t.Errorf("second message: got %q", msgs[1])
	}
}

func TestContentText(t *testing.T) {
	blocks := []json.RawMessage{
		json.RawMessage(`{"type":"text","text":"a"}`),
		json.RawMessage(`{"type":"text","text":"b"}`),
		json.RawMessage(`{"type":"image","data":"zzz"}`),
	}
	got := contentText(blocks)
	if !strings.HasPrefix(got, "a\nb\n") || !strings.Contains(got, `"type":"image"`) {
		t.Errorf("unexpected content text: %q", got)
	}
}
