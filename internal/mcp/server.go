package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// Handler is the server-side tool provider: one MCP server's brain.
// Transports (stdio, streamable HTTP) share it; the agentkit tool
// surface is the production implementation (ADR-0017).
type Handler interface {
	// ProtocolVersion reports the MCP revision the server speaks.
	ProtocolVersion() string
	// ServerInfo names the server for the initialize reply.
	ServerInfo() (name, version string)
	// Tools lists the served tools.
	Tools() []ToolInfo
	// CallTool executes one tool call: (text content, isError, err).
	// A non-nil err becomes a JSON-RPC error reply (transport-level
	// failure); isError=true is a tool-level failure with content.
	CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error)
}

// dispatch routes one JSON-RPC request/notification against h and
// renders the reply (nil = no reply: notifications). ctx bounds the
// tool call (the HTTP transport passes the request context so a
// disconnected agent cancels an approval wait).
func dispatch(ctx context.Context, h Handler, line []byte) ([]byte, error) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return marshalRPC(rpcResponse{
			JSONRPC: "2.0", ID: 0,
			Error: &rpcError{Code: -32700, Message: "parse error"},
		})
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		name, version := h.ServerInfo()
		result := map[string]any{
			"protocolVersion": h.ProtocolVersion(),
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": name, "version": version},
		}
		return resultRPC(req.ID, result)
	case "notifications/initialized":
		return nil, nil
	case "tools/list":
		return resultRPC(req.ID, map[string]any{"tools": h.Tools()})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return resultRPC(req.ID, toolError("malformed arguments"))
		}
		text, isErr, err := h.CallTool(ctx, p.Name, p.Arguments)
		if err != nil {
			return resultRPC(req.ID, toolError(err.Error()))
		}
		return resultRPC(req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": isErr,
		})
	default:
		if req.ID == 0 {
			return nil, nil // unknown notification: ignore
		}
		return marshalRPC(rpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method},
		})
	}
}

func toolError(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": true,
	}
}

func marshalRPC(v rpcResponse) ([]byte, error) {
	return json.Marshal(v)
}

func resultRPC(id int64, result any) ([]byte, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Result: raw})
}

// ServeStdio serves h over newline-framed JSON-RPC on r→w until r is
// exhausted (the stdio transport: tests, and the future helper shape).
func ServeStdio(r io.Reader, w io.Writer, h Handler) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		reply, err := dispatch(context.Background(), h, sc.Bytes())
		if err != nil {
			return err
		}
		if reply == nil {
			continue
		}
		if _, err := w.Write(append(reply, '\n')); err != nil {
			return err
		}
	}
	return sc.Err()
}

// HTTPHandler renders h as a streamable-HTTP endpoint: JSON-RPC POSTs
// answered with single application/json bodies (notifications → 202).
func HTTPHandler(h Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Streamable-HTTP clients (opencode, claude) open a GET SSE
		// stream for server-initiated messages. DHI's server sends
		// none, but a 405 here makes clients treat the endpoint as
		// dead and silently drop every served tool, so we hold the
		// stream open for the session's lifetime instead.
		if r.Method == http.MethodGet {
			if !strings.Contains(r.Header.Get("accept"), "text/event-stream") {
				http.Error(w, "GET requires Accept: text/event-stream", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("content-type", "text/event-stream")
			w.Header().Set("cache-control", "no-cache")
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read: "+err.Error(), http.StatusBadRequest)
			return
		}
		reply, err := dispatch(r.Context(), h, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if reply == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// Streamable-HTTP clients that accept SSE require the POST
		// reply framed as an SSE `message` event (opencode rejects a
		// bare JSON body and times out). Clients that ask for only
		// application/json get a plain body.
		if strings.Contains(r.Header.Get("accept"), "text/event-stream") {
			w.Header().Set("content-type", "text/event-stream")
			w.Header().Set("cache-control", "no-cache")
			_, _ = w.Write([]byte("event: message\ndata: "))
			_, _ = w.Write(reply)
			_, _ = w.Write([]byte("\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write(reply)
	})
}

// ServeLoopback starts h on an ephemeral 127.0.0.1 port and returns the
// endpoint + a stop func. The lifetime is one agent turn (ADR-0017:
// no daemon; the listener exists only while the CLI child runs).
func ServeLoopback(h Handler) (endpoint string, stop func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("mcp: loopback listen: %w", err)
	}
	srv := &http.Server{Handler: HTTPHandler(h)}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String() + "/mcp", func() { _ = srv.Close() }, nil
}
