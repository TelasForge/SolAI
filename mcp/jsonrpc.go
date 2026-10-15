package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// protocolVersion is the MCP spec revision this client speaks.
const protocolVersion = "2025-06-18"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *rpcError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("mcp: rpc error %d: %s (%s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("mcp: rpc error %d: %s", e.Code, e.Message)
}

type remoteTool struct {
	name        string
	description string
	schema      map[string]any
}

// httpTransport speaks JSON-RPC over the MCP Streamable HTTP transport.
type httpTransport struct {
	url     string
	headers map[string]string
	http    *http.Client

	sessionID string
	version   string
	nextID    atomic.Int64
}

func newHTTPTransport(s Server) *httpTransport {
	return &httpTransport{
		url:     s.URL,
		headers: s.Headers,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// initialize performs the MCP handshake.
func (t *httpTransport) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "solai", "version": "0.1.0"},
	}
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := t.call(ctx, "initialize", params, &result); err != nil {
		return err
	}
	t.version = result.ProtocolVersion
	if t.version == "" {
		t.version = protocolVersion
	}
	return t.notify(ctx, "notifications/initialized", nil)
}

func (t *httpTransport) listTools(ctx context.Context) ([]remoteTool, error) {
	var result struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := t.call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	out := make([]remoteTool, 0, len(result.Tools))
	for _, tt := range result.Tools {
		out = append(out, remoteTool{name: tt.Name, description: tt.Description, schema: tt.InputSchema})
	}
	return out, nil
}

func (t *httpTransport) callTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	params := struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}{Name: name, Arguments: args}

	var result struct {
		Content []json.RawMessage `json:"content"`
		IsError bool              `json:"isError"`
	}
	if err := t.call(ctx, "tools/call", params, &result); err != nil {
		return "", false, err
	}
	return contentText(result.Content), result.IsError, nil
}

// call sends a request and decodes the matching response.
func (t *httpTransport) call(ctx context.Context, method string, params any, out any) error {
	id := t.nextID.Add(1)
	body, err := t.encode(id, method, params)
	if err != nil {
		return err
	}
	msgs, err := t.do(ctx, body)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		var r rpcResponse
		if err := json.Unmarshal(m, &r); err != nil {
			continue
		}
		if r.ID != id {
			continue
		}
		if r.Error != nil {
			return r.Error
		}
		if out != nil {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	}
	return fmt.Errorf("mcp: no response for %s", method)
}

// notify sends a notification (no id, no response body).
func (t *httpTransport) notify(ctx context.Context, method string, params any) error {
	body, err := t.encode(0, method, params)
	if err != nil {
		return err
	}
	_, err = t.do(ctx, body)
	return err
}

func (t *httpTransport) encode(id int64, method string, params any) ([]byte, error) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	return json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: raw})
}

// do POSTs a message and returns the JSON-RPC messages in the response. The
// server may reply with a single JSON document or an SSE stream.
func (t *httpTransport) do(ctx context.Context, body []byte) ([]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	if t.version != "" {
		req.Header.Set("MCP-Protocol-Version", t.version)
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: request failed: %w", err)
	}
	defer resp.Body.Close()

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.sessionID = sid
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("mcp: reading response: %w", err)
	}
	if resp.StatusCode == http.StatusAccepted {
		return nil, nil // notification accepted
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mcp: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return parseSSE(data), nil
	}
	if len(data) == 0 {
		return nil, nil
	}
	return []json.RawMessage{data}, nil
}

// parseSSE extracts the `data:` payloads from an SSE response.
func parseSSE(data []byte) []json.RawMessage {
	var out []json.RawMessage
	var cur strings.Builder

	flush := func() {
		if cur.Len() > 0 {
			out = append(out, json.RawMessage(cur.String()))
			cur.Reset()
		}
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "data:"):
			cur.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
	return out
}

// contentText reduces MCP content blocks to a single string. Text blocks are
// joined; anything else is passed through as its raw JSON.
func contentText(blocks []json.RawMessage) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		var cb struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(b, &cb) == nil && cb.Type == "text" && cb.Text != "" {
			parts = append(parts, cb.Text)
			continue
		}
		parts = append(parts, string(b))
	}
	return strings.Join(parts, "\n")
}
