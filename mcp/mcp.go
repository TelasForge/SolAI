// Package mcp is a minimal, dependency-free Model Context Protocol client.
//
// It speaks JSON-RPC 2.0 over the Streamable HTTP transport, discovers a
// server's tools, and adapts them to llm.Tool so the coordinator can call them
// like any other tool.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/melodyogonna/solai/llm"
)

// Server describes an MCP server to connect to.
type Server struct {
	// Name namespaces the server's tools (e.g. "jupiter" -> "jupiter__quote").
	Name string

	// URL is the Streamable HTTP endpoint.
	URL string

	// Headers are sent with every request (e.g. Authorization).
	Headers map[string]string
}

// Client holds live connections to MCP servers and their adapted tools.
type Client struct {
	conns []*conn
	tools []llm.Tool
}

// Connect connects to every server and returns a client. On error, any
// connections already made are closed. The caller must Close the client.
func Connect(ctx context.Context, servers []Server) (*Client, error) {
	c := &Client{}
	for _, s := range servers {
		cn, err := connect(ctx, s)
		if err != nil {
			_ = c.Close()
			return nil, err
		}
		c.conns = append(c.conns, cn)
		for _, t := range cn.tools {
			c.tools = append(c.tools, t)
		}
	}
	return c, nil
}

// Tools returns the adapted tools from all connected servers.
func (c *Client) Tools() []llm.Tool { return c.tools }

// Close releases the connections. The Streamable HTTP transport carries no
// server-side session to tear down, so this is a no-op today; it exists so
// transports with real lifetimes can be added later.
func (c *Client) Close() error { return nil }

func connect(ctx context.Context, s Server) (*conn, error) {
	if s.Name == "" {
		return nil, errors.New("mcp: server name is required")
	}
	if s.URL == "" {
		return nil, fmt.Errorf("mcp: server %q has no URL", s.Name)
	}

	t := newHTTPTransport(s)
	if err := t.initialize(ctx); err != nil {
		return nil, fmt.Errorf("mcp: %s: initialize: %w", s.Name, err)
	}
	remote, err := t.listTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: tools/list: %w", s.Name, err)
	}

	cn := &conn{name: s.Name, transport: t}
	for _, rt := range remote {
		cn.tools = append(cn.tools, &mcpTool{conn: cn, remote: rt})
	}
	return cn, nil
}

// conn is one live server connection.
type conn struct {
	name      string
	transport *httpTransport
	tools     []*mcpTool
}

// mcpTool adapts a remote MCP tool to llm.Tool.
type mcpTool struct {
	conn   *conn
	remote remoteTool
}

func (t *mcpTool) Name() string { return t.conn.name + "__" + sanitize(t.remote.name) }

func (t *mcpTool) Description() string { return t.remote.description }

func (t *mcpTool) Parameters() map[string]any {
	if t.remote.schema == nil {
		return llm.OpenObjectSchema()
	}
	return t.remote.schema
}

func (t *mcpTool) Call(ctx context.Context, argsJSON string) (string, error) {
	args := json.RawMessage(argsJSON)
	if !json.Valid(args) {
		args = json.RawMessage("{}")
	}
	out, isError, err := t.conn.transport.callTool(ctx, t.remote.name, args)
	if err != nil {
		return "", err
	}
	if isError {
		return "Tool error: " + out, nil
	}
	return out, nil
}

// sanitize maps a name into the character set allowed for function names.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Names returns the namespaced tool names, sorted, for diagnostics.
func (c *Client) Names() []string {
	out := make([]string, 0, len(c.tools))
	for _, t := range c.tools {
		out = append(out, t.Name())
	}
	sort.Strings(out)
	return out
}
