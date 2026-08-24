// Package mcp implements a minimal Model Context Protocol client so DHI
// agents can reach external tool servers (F-007). Both supported
// transports — stdio (line-delimited JSON-RPC over a child process) and
// streamable HTTP (POST with JSON or SSE replies) — share the same
// request/response core and surface identical semantics: list tools,
// call a tool, close the session. Servers never bypass the agent sandbox;
// bridging happens in internal/agentkit/tools.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ProtocolVersion is the MCP revision this client speaks.
const ProtocolVersion = "2024-11-05"

// ToolInfo describes one tool exposed by a server.
type ToolInfo struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Caller is the transport-neutral surface agents consume.
type Caller interface {
	// Tools lists the server's tools.
	Tools(ctx context.Context) ([]ToolInfo, error)
	// CallTool invokes one tool; content is its textual output and
	// isError mirrors the server's flag.
	CallTool(ctx context.Context, name string, args json.RawMessage) (content string, isError bool, err error)
	// Close releases the transport.
	Close() error
}

// rpcRequest is one JSON-RPC 2.0 request or notification.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcError is the JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) err() error {
	return fmt.Errorf("mcp: rpc %d: %s", e.Code, e.Message)
}

// rpcResponse is one JSON-RPC reply.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// conn serializes request writes and correlates replies by id. It is
// embedded by both transports.
type conn struct {
	mu     sync.Mutex
	w      writer
	nextID int64

	muOut   sync.Mutex
	pending map[int64]chan rpcResponse
}

type writer interface {
	write([]byte) error
}

func newConn(w writer) *conn {
	return &conn{w: w, pending: map[int64]chan rpcResponse{}}
}

// call sends one request and waits for its reply; ctx bounds the wait.
func (c *conn) call(ctx context.Context, method string, params any, out any) error {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("mcp: encode params: %w", err)
		}
		raw = b
	}
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResponse, 1)
	c.muOut.Lock()
	c.pending[id] = ch
	c.muOut.Unlock()
	req, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: raw})
	if err != nil {
		c.abandon(id)
		c.mu.Unlock()
		return fmt.Errorf("mcp: encode request: %w", err)
	}
	if err := c.w.write(req); err != nil {
		c.abandon(id)
		c.mu.Unlock()
		return fmt.Errorf("mcp: send %s: %w", method, err)
	}
	c.mu.Unlock()

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error.err()
		}
		if out != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("mcp: decode %s result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		c.abandon(id)
		return ctx.Err()
	}
}

// abandon drops a pending call slot.
func (c *conn) abandon(id int64) {
	c.muOut.Lock()
	delete(c.pending, id)
	c.muOut.Unlock()
}

// notify sends a notification (no id, no reply).
func (c *conn) notify(method string, params any) error {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("mcp: encode params: %w", err)
		}
		raw = b
	}
	req, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: raw})
	if err != nil {
		return fmt.Errorf("mcp: encode notification: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.w.write(req); err != nil {
		return fmt.Errorf("mcp: send %s: %w", method, err)
	}
	return nil
}

// handleLine routes one incoming message: replies wake their caller,
// notifications and unknown payloads are dropped (no server-initiated
// features are consumed in M3).
func (c *conn) handleLine(line []byte) {
	var resp rpcResponse
	if err := json.Unmarshal(line, &resp); err != nil || resp.ID == 0 {
		return
	}
	c.muOut.Lock()
	ch, ok := c.pending[resp.ID]
	delete(c.pending, resp.ID)
	c.muOut.Unlock()
	if ok {
		ch <- resp
	}
}

// initializeParams/Result implement the mandatory handshake.
type initializeParams struct {
	ProtocolVersion string      `json:"protocolVersion"`
	Capabilities    struct{}    `json:"capabilities"`
	ClientInfo      clientIdent `json:"clientInfo"`
}

type clientIdent struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	ServerInfo      struct {
		Name string `json:"name"`
	} `json:"serverInfo"`
}

type toolsResult struct {
	Tools []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	} `json:"tools"`
}

type callResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// handshake performs initialize + initialized notification.
func (c *conn) handshake(ctx context.Context) error {
	var res initializeResult
	ip := initializeParams{ProtocolVersion: ProtocolVersion, ClientInfo: clientIdent{Name: "dhi", Version: "3"}}
	if err := c.call(ctx, "initialize", ip, &res); err != nil {
		return fmt.Errorf("mcp: initialize: %w", err)
	}
	return c.notify("notifications/initialized", nil)
}

// listTools fetches and converts the server's tool inventory.
func (c *conn) listTools(ctx context.Context) ([]ToolInfo, error) {
	var res toolsResult
	if err := c.call(ctx, "tools/list", nil, &res); err != nil {
		return nil, fmt.Errorf("mcp: tools/list: %w", err)
	}
	out := make([]ToolInfo, 0, len(res.Tools))
	for _, t := range res.Tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		out = append(out, ToolInfo{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return out, nil
}

// callTool executes one remote tool and flattens its text content.
func (c *conn) callTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	params := struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}{Name: name, Arguments: args}
	var res callResult
	if err := c.call(ctx, "tools/call", params, &res); err != nil {
		return "", false, err
	}
	return flatten(res), res.IsError, nil
}

// flatten joins text content blocks with newlines.
func flatten(res callResult) string {
	var sb strings.Builder
	for i, blk := range res.Content {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(blk.Text)
	}
	return sb.String()
}
