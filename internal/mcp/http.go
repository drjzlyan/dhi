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
	"sync"
)

// HTTP speaks the streamable-HTTP transport: JSON-RPC POSTs answered
// either with a single application/json body or a text/event-stream of
// messages. Server-initiated SSE streams (GET) are not consumed; M3
// clients only call tools.
type HTTP struct {
	endpoint string
	http     *http.Client

	mu      sync.Mutex
	session string // mcp-session-id issued on initialize, if any
	conn    *conn
}

// DialHTTP connects to an MCP streamable-HTTP endpoint and handshakes.
func DialHTTP(ctx context.Context, endpoint string, client *http.Client) (*HTTP, error) {
	if client == nil {
		client = http.DefaultClient
	}
	h := &HTTP{endpoint: endpoint, http: client}
	h.conn = newConn(h)
	if err := h.conn.handshake(ctx); err != nil {
		return nil, err
	}
	return h, nil
}

// write implements conn.writer via one HTTP round trip. Replies may be
// plain JSON or an SSE stream carrying the response.
func (h *HTTP) write(payload []byte) error {
	req, err := http.NewRequest(http.MethodPost, h.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("mcp/http: build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	h.mu.Lock()
	if h.session != "" {
		req.Header.Set("mcp-session-id", h.session)
	}
	h.mu.Unlock()
	resp, err := h.http.Do(req)
	if err != nil {
		return fmt.Errorf("mcp/http: post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if sid := resp.Header.Get("mcp-session-id"); sid != "" {
		h.mu.Lock()
		if h.session == "" {
			h.session = sid
		}
		h.mu.Unlock()
	}
	switch {
	case resp.StatusCode != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("mcp/http: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	case strings.HasPrefix(resp.Header.Get("content-type"), "text/event-stream"):
		return h.consumeSSE(resp.Body)
	default:
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return fmt.Errorf("mcp/http: read body: %w", err)
		}
		h.conn.handleLine(body)
		return nil
	}
}

// consumeSSE scans data lines for the responses our pending calls need;
// the stream ends when the server closes it.
func (h *HTTP) consumeSSE(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data:") {
			h.conn.handleLine([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))))
		}
	}
	return sc.Err()
}

// Tools implements Caller.
func (h *HTTP) Tools(ctx context.Context) ([]ToolInfo, error) { return h.conn.listTools(ctx) }

// CallTool implements Caller.
func (h *HTTP) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	return h.conn.callTool(ctx, name, args)
}

// Close is a no-op for the stateless HTTP transport (session ids expire
// server-side).
func (h *HTTP) Close() error { return nil }
