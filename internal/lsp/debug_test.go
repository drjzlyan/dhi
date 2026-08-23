package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestDebugManualPair(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	srv := &fakeServer{conn: serverConn, reader: bufio.NewReader(serverConn), t: t}
	go srv.serve()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *Client, 1)
	go func() {
		c, err := New(ctx, clientConn, "/ws")
		if err != nil {
			t.Errorf("init: %v", err)
			done <- nil
			return
		}
		done <- c
	}()

	select {
	case c := <-done:
		if c == nil {
			t.FailNow()
		}
		t.Log("handshake OK")
	case <-time.After(2 * time.Second):
		t.Fatal("handshake hung")
	}
}

func TestDebugDecode(t *testing.T) {
	params := json.RawMessage(`{"uri":"file:///ws/a.go","diagnostics":[{"range":{"start":{"line":2,"character":4}},"severity":1,"message":"undefined: x"}]}`)
	ev, ok := decodeDiagnostics(params)
	t.Logf("ok=%v ev=%+v", ok, ev)
}

func TestDebugPushBlocking(t *testing.T) {
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

	pushed := make(chan struct{})
	go func() {
		srv.push("textDocument/publishDiagnostics", map[string]any{
			"uri":         "file:///ws/a.go",
			"diagnostics": []map[string]any{},
		})
		close(pushed)
	}()
	select {
	case <-pushed:
		t.Log("push delivered")
	case <-time.After(1 * time.Second):
		t.Fatal("push BLOCKED — reader not consuming")
	}

	select {
	case ev := <-c2.Events:
		t.Logf("event kind=%v diags=%d", ev.Kind, len(ev.Diags))
	case <-time.After(1 * time.Second):
		t.Fatal("no event")
	}
}
