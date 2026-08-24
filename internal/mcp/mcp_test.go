package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// pipeClient wires a client-side conn to an in-process fake server over
// net.Pipe, exercising the full JSON-RPC path without spawning anything.
func pipeClient(t *testing.T, handle func(method string, params json.RawMessage) (any, bool)) *conn {
	t.Helper()
	client, server := net.Pipe()
	c := newConn(lineWriter{client})
	done := make(chan struct{})
	t.Cleanup(func() { close(done); client.Close(); server.Close() })
	// Pump replies into the conn exactly as each transport's read loop does.
	go func() {
		sc := bufio.NewScanner(client)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			c.handleLine(sc.Bytes())
		}
	}()
	go func() {
		sc := bufio.NewScanner(server)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			var req rpcRequest
			if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
				continue
			}
			result, ok := handle(req.Method, req.Params)
			var resp []byte
			if !ok {
				resp, _ = json.Marshal(rpcResponse{JSONRPC: "2.0", ID: req.ID,
					Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
			} else {
				b, _ := json.Marshal(result)
				resp, _ = json.Marshal(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: b})
			}
			server.Write(append(resp, '\n'))
		}
	}()
	return c
}

func TestHandshakeAndTools(t *testing.T) {
	ctx := context.Background()
	c := pipeClient(t, func(method string, _ json.RawMessage) (any, bool) {
		switch method {
		case "initialize":
			return map[string]any{"protocolVersion": ProtocolVersion}, true
		case "tools/list":
			return map[string]any{"tools": []map[string]any{
				{"name": "lookup", "description": "find docs", "inputSchema": map[string]any{"type": "object"}},
			}}, true
		default:
			return nil, false
		}
	})
	if err := c.handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	tools, err := c.listTools(ctx)
	if err != nil {
		t.Fatalf("listTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "lookup" || tools[0].Description != "find docs" {
		t.Errorf("tools = %+v", tools)
	}
}

func TestCallToolFlattensContent(t *testing.T) {
	ctx := context.Background()
	c := pipeClient(t, func(method string, params json.RawMessage) (any, bool) {
		if method == "initialize" {
			return map[string]any{}, true
		}
		if method != "tools/call" {
			return nil, false
		}
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		json.Unmarshal(params, &p)
		if p.Name != "calc" {
			return map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": "no calc"}}}, true
		}
		return map[string]any{"content": []map[string]string{
			{"type": "text", "text": "line1"},
			{"type": "text", "text": "line2"},
		}}, true
	})
	c.nextID = 100 // distinct id space from handshake-less tests
	content, isErr, err := c.callTool(ctx, "calc", json.RawMessage(`{"x":1}`))
	if err != nil || isErr {
		t.Fatalf("callTool: content=%q isErr=%v err=%v", content, isErr, err)
	}
	if content != "line1\nline2" {
		t.Errorf("content = %q", content)
	}
	content, isErr, err = c.callTool(ctx, "missing", nil)
	if err != nil {
		t.Fatalf("callTool missing: %v", err)
	}
	if !isErr || content != "no calc" {
		t.Errorf("missing tool: content=%q isErr=%v", content, isErr)
	}
}

func TestUnknownMethodError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c := pipeClient(t, func(string, json.RawMessage) (any, bool) { return nil, false })
	err := c.call(ctx, "bogus/method", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "32601") {
		t.Errorf("err = %v, want method-not-found", err)
	}
}

func TestStdioTransport(t *testing.T) {
	// awk keeps a stateful JSON-RPC responder tiny and dependency-free.
	script := `{ match($0, /"id":[0-9]+/); id=substr($0, RSTART+5, RLENGTH-5);
	if ($0 ~ /initialize/) print "{\"jsonrpc\":\"2.0\",\"id\":" id ",\"result\":{\"protocolVersion\":\"2024-11-05\",\"serverInfo\":{\"name\":\"fake\",\"version\":\"1\"}}}";
	else if ($0 ~ /tools\/list/) print "{\"jsonrpc\":\"2.0\",\"id\":" id ",\"result\":{\"tools\":[{\"name\":\"lookup\",\"inputSchema\":{\"type\":\"object\"}}]}}";
	else if ($0 ~ /tools\/call/) print "{\"jsonrpc\":\"2.0\",\"id\":" id ",\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"42\"}]}}";
	fflush(); }`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := DialStdio(context.Background(), nil, "awk", script)
	if err != nil {
		t.Fatalf("DialStdio: %v", err)
	}
	defer s.Close()
	tools, err := s.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "lookup" {
		t.Errorf("tools = %+v", tools)
	}
	content, isErr, err := s.CallTool(ctx, "lookup", json.RawMessage(`{"q":"x"}`))
	if err != nil || isErr || content != "42" {
		t.Errorf("call = %q isErr=%v err=%v", content, isErr, err)
	}
}

func TestHTTPTransport(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in rpcRequest
		json.Unmarshal(body, &in)
		methods = append(methods, in.Method)
		w.Header().Set("mcp-session-id", "sess-1")
		w.Header().Set("content-type", "application/json")
		var result any
		switch in.Method {
		case "initialize":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "remote_tool"}}}
		case "tools/call":
			result = map[string]any{"content": []map[string]string{{"type": "text", "text": "hi from http"}}}
		}
		out, _ := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: in.ID, Result: mustJSON(t, result)})
		w.Write(out)
	}))
	defer srv.Close()

	h, err := DialHTTP(context.Background(), srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("DialHTTP: %v", err)
	}
	defer h.Close()
	tools, err := h.Tools(context.Background())
	if err != nil || len(tools) != 1 || tools[0].Name != "remote_tool" {
		t.Fatalf("tools = %+v err = %v", tools, err)
	}
	content, _, err := h.CallTool(context.Background(), "remote_tool", nil)
	if err != nil || content != "hi from http" {
		t.Errorf("call = %q err = %v", content, err)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
