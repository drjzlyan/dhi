package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeServer implements just enough LSP over one side of net.Pipe.
type fakeServer struct {
	conn      net.Conn
	reader    *bufio.Reader
	t         *testing.T
	initDone  bool
	onRequest func(method string, params json.RawMessage) (any, bool)
}

func startFake(t *testing.T, onRequest func(string, json.RawMessage) (any, bool)) *Client {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	fs := &fakeServer{conn: serverConn, reader: bufio.NewReader(serverConn), t: t, onRequest: onRequest}
	go fs.serve()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c, err := New(ctx, clientConn, "/ws")
	if err != nil {
		t.Fatalf("client init: %v", err)
	}
	t.Cleanup(func() { c.Shutdown() })
	return c
}

func (f *fakeServer) serve() {
	for {
		payload, err := readFrame(f.reader)
		if err != nil {
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		switch {
		case msg.Method == "initialize":
			result := map[string]any{"serverInfo": map[string]any{"name": "fake-lsp"}}
			f.reply(*msg.ID, result)
			f.initDone = true
		case msg.ID != nil && msg.Method != "":
			var result any = nil
			if f.onRequest != nil {
				if r, handled := f.onRequest(msg.Method, msg.Params); handled {
					result = r
				}
			}
			f.reply(*msg.ID, result)
		default:
			// notifications ignored
		}
	}
}

func (f *fakeServer) reply(id int64, result any) {
	data, _ := json.Marshal(rpcMessage{JSONRPC: "2.0", ID: &id, Result: mustJSON(result)})
	writeFrame(f.conn, data)
}

// push sends a notification from server to client.
func (f *fakeServer) push(method string, params any) {
	data, _ := encodeNotification(method, params)
	writeFrame(f.conn, data)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func waitEvent(t *testing.T, c *Client) Event {
	t.Helper()
	select {
	case ev, ok := <-c.Events:
		if !ok {
			t.Fatal("events channel closed")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
		return Event{}
	}
}

func TestInitializeHandshake(t *testing.T) {
	c := startFake(t, nil)
	if c.ServerName() != "fake-lsp" {
		t.Errorf("server name = %q", c.ServerName())
	}
	if err := c.DidOpen("/ws/a.go", "package main\n", "go"); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticsStream(t *testing.T) {
	// Simpler: create client+pipe manually.
	clientConn, serverConn := net.Pipe()
	srv := &fakeServer{conn: serverConn, reader: bufio.NewReader(serverConn), t: t}
	go srv.serve()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c2, err := New(ctx, clientConn, "/ws")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Shutdown()

	srv.push("textDocument/publishDiagnostics", map[string]any{
		"uri": "file:///ws/a.go",
		"diagnostics": []map[string]any{
			{"range": map[string]any{"start": map[string]any{"line": 2, "character": 4}}, "severity": 1, "message": "undefined: x"},
		},
	})

	ev := waitEvent(t, c2)
	if ev.Kind != EvDiagnostics || len(ev.Diags) != 1 {
		t.Fatalf("event = %+v", ev)
	}
	d := ev.Diags[0]
	if d.Path != "/ws/a.go" || d.Line != 2 || d.Col != 4 || d.Severity != 1 {
		t.Errorf("diag = %+v", d)
	}
	if !strings.Contains(d.Message, "undefined") {
		t.Errorf("message = %q", d.Message)
	}
}

func TestCompletionRoundTrip(t *testing.T) {
	c := startFake(t, func(method string, params json.RawMessage) (any, bool) {
		if method == "textDocument/completion" {
			return map[string]any{"items": []map[string]any{
				{"label": "Println", "detail": "func(a ...any)"},
				{"label": "Printf"},
			}}, true
		}
		return nil, false
	})
	items, err := c.Completion("/ws/a.go", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Label != "Println" || items[1].Detail != "" {
		t.Fatalf("items = %+v", items)
	}
}

func TestCompletionArrayShapeResult(t *testing.T) {
	c := startFake(t, func(string, json.RawMessage) (any, bool) {
		return []map[string]any{{"label": "only"}}, true
	})
	items, err := c.Completion("/ws/a.go", 0, 0)
	if err != nil || len(items) != 1 || items[0].Label != "only" {
		t.Errorf("items = %+v err = %v", items, err)
	}
}

func TestShutdownClosesEvents(t *testing.T) {
	c := startFake(t, nil)
	c.Shutdown()
	select {
	case _, ok := <-c.Events:
		if ok {
			t.Error("events should close after shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("events not closed after shutdown")
	}
}

var _ io.ReadWriteCloser = (net.Conn)(nil)
